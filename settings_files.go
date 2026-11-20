package turborel

// A connection URI, and the files connection settings come from [CLIENT
// The connection URI] [CLIENT The service file] [CLIENT The password
// file].
//
//	turborel://[user[:password]@][host[:port][,host[:port]]...][/][?key=value[&key=value]...]
//
// A URI's user, password, hosts and values are percent-encoded (RFC 3986
// §2.1), so a host %2Frun%2Fturborel.sock names a Unix socket, and an
// IPv6 address is written in brackets. Each key is a setting's name. A
// server serves one database, so the URI names none, and its path is
// empty, or / alone.
//
// A service file holds sections, each [name] on a line of its own, then
// lines key = value, each key a setting's name but service and
// service_file. A line beginning with # is a comment; blank lines are
// skipped. A password file holds lines host:port:user:password, a field
// * matching anything, \ taking the character after it as it is, so \:
// is a colon and \\ a backslash; the first line that matches is taken,
// and a line of fewer fields is skipped. A password file should be
// readable by its owner alone; the driver checks only that it is a
// regular file, as turborel-client does, and its owner answers for the
// rest.

import (
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// URIScheme is the scheme a connection URI begins with.
const URIScheme = "turborel://"

// The text the files are written in.
const (
	commentMark    = "#"
	anyField       = "*"
	escapeMark     = '\\'
	fieldSeparator = ':'
	passwordFields = 4
	hexBase        = 16
)

// decode gives text percent-decoded, as UTF-8.
func decode(text string) (string, error) {
	var octets []byte

	for i := 0; i < len(text); i++ {
		if text[i] != '%' {
			octets = append(octets, text[i])

			continue
		}

		var b byte

		digits := text[i+1 : min(i+1+len("XX"), len(text))]

		ok := len(digits) == len("XX")
		if ok {
			n, err := strconv.ParseUint(digits, hexBase, 8)
			ok, b = err == nil, byte(n)
		}

		if !ok {
			return "", invalid(text + " has a % without two hexadecimal digits")
		}

		octets = append(octets, b)
		i += len("XX")
	}

	if !utf8.Valid(octets) {
		return "", invalid(text + " is not UTF-8 once decoded")
	}

	return string(octets), nil
}

// OptionsFromURI gives the settings the connection URI uri gives. It
// fails with SQLSTATE 08001 if it is not one: another scheme, a path, a
// key that names no setting, a value without =, or bad percent-encoding.
func OptionsFromURI(uri string) (Options, error) {
	var o Options

	rest, ok := strings.CutPrefix(uri, URIScheme)
	if !ok {
		return o, invalid("a connection URI begins with " + URIScheme)
	}

	before, query, _ := strings.Cut(rest, "?")
	authority, path, _ := strings.Cut(before, "/")

	if path != "" {
		return o, invalid("a connection URI names no database, so has no path, not /" + path)
	}

	userinfo, hosts := "", authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo, hosts = authority[:at], authority[at+1:]
	}

	if err := o.setUser(userinfo); err != nil {
		return o, err
	}

	if hosts != "" {
		if err := o.setDecoded(OptionHost, hosts); err != nil {
			return o, err
		}
	}

	return o, o.setQuery(query)
}

// setQuery sets the settings a URI's query gives.
func (o *Options) setQuery(query string) error {
	for pair := range strings.SplitSeq(query, "&") {
		if pair == "" {
			continue
		}

		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return invalid(pair + " in a connection URI has no =")
		}

		name, err := decode(key)
		if err != nil {
			return err
		}

		if err := o.setDecoded(name, value); err != nil {
			return err
		}
	}

	return nil
}

// setUser sets the user and password a URI's userinfo gives.
func (o *Options) setUser(userinfo string) error {
	if userinfo == "" {
		return nil
	}

	user, password, hasPassword := strings.Cut(userinfo, ":")
	if err := o.setDecoded(OptionUser, user); err != nil {
		return err
	}

	if hasPassword {
		return o.setDecoded(OptionPassword, password)
	}

	return nil
}

// setDecoded sets key to value percent-decoded.
func (o *Options) setDecoded(key, value string) error {
	decoded, err := decode(value)
	if err != nil {
		return err
	}

	return o.Set(key, decoded)
}

// Service gives the settings of the section name of the service file
// file. It fails with SQLSTATE 08001 if the file cannot be read, a line
// is malformed, or it has no section name.
func Service(file, name string) (Options, error) {
	text, err := os.ReadFile(file) //nolint:gosec // G304: the service file the settings name.
	if err != nil {
		return Options{}, invalid("cannot read " + file + ": " + err.Error())
	}

	return serviceOf(string(text), name, file)
}

// serviceOf gives the settings of the section name of the service file
// text, read from file.
func serviceOf(text, name, file string) (Options, error) {
	var (
		found   Options
		matched bool
		current *string
	)

	for i, line := range strings.Split(text, "\n") {
		section, setting, why := serviceLine(strings.TrimSpace(line), current)
		if why != "" {
			return Options{}, invalid(file + " line " + strconv.Itoa(i+1) + ": " + why)
		}

		if section != nil {
			current = section
		}

		if setting != nil && *current == name {
			found, matched = setting.Over(found), true
		}
	}

	if !matched {
		return Options{}, invalid(file + " has no service " + name)
	}

	return found, nil
}

// serviceLine reads a service file's line, within the section current,
// if any: the section it begins, or the setting it sets, or why it is
// malformed; nothing of a comment or a blank line.
func serviceLine(line string, current *string) (*string, *Options, string) {
	if line == "" || strings.HasPrefix(line, commentMark) {
		return nil, nil, ""
	}

	if section, ok := strings.CutPrefix(line, "["); ok {
		section, ok = strings.CutSuffix(section, "]")
		if !ok {
			return nil, nil, "a section's name ends with ]"
		}

		section = strings.TrimSpace(section)

		return &section, nil, ""
	}

	if current == nil {
		return nil, nil, "a setting comes before any section"
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return nil, nil, "a setting is <key> = <value>"
	}

	key = strings.TrimSpace(key)
	if key == OptionService || key == OptionServiceFile {
		return nil, nil, "a service cannot set " + key
	}

	var setting Options
	if err := setting.Set(key, strings.TrimSpace(value)); err != nil {
		return nil, nil, asError(err).Message
	}

	return nil, &setting, ""
}

// passwordField is a password file's field, unescaped, and whether it was
// written with no escape.
type passwordField struct {
	value string
	plain bool
}

// passwordFieldsOf gives the fields of a password file's line, unescaped:
// the first three split at each colon not escaped, the fourth the rest.
func passwordFieldsOf(line string) []passwordField {
	var (
		fields  []passwordField
		field   strings.Builder
		escaped bool
	)

	plain := true

	for _, c := range line {
		switch {
		case escaped:
			field.WriteRune(c)
			escaped, plain = false, false
		case c == escapeMark:
			escaped = true
		case c == fieldSeparator && len(fields) < passwordFields-1:
			fields = append(fields, passwordField{value: field.String(), plain: plain})
			field.Reset()

			plain = true
		default:
			field.WriteRune(c)
		}
	}

	return append(fields, passwordField{value: field.String(), plain: plain})
}

// matches says whether the field matches wanted: a * written plain
// matches anything.
func (f passwordField) matches(wanted string) bool {
	return f.plain && f.value == anyField || f.value == wanted
}

// passwordOf gives the password the password file text gives user at
// host and port: that of its first line matching all three.
func passwordOf(text, host, port, user string) (string, bool) {
	for line := range strings.Lines(text) {
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), commentMark) {
			continue
		}

		f := passwordFieldsOf(line)
		if len(f) == passwordFields && f[0].matches(host) && f[1].matches(port) && f[2].matches(user) {
			return f[3].value, true
		}
	}

	return "", false
}

// readRegular gives the text of the file at path, if it can be read and
// is a regular file, a link followed to one.
func readRegular(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}

	text, err := os.ReadFile(path) //nolint:gosec // G304: the password file the settings name.
	if err != nil {
		return "", false
	}

	return string(text), true
}
