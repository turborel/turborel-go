package turborel

import (
	"strings"
	"testing"
)

// TestAttributesBounded takes attributes within the bounds and refuses
// those past them.
//
// Verifies: [PROTOCOL The tail] a message takes at most 32 attributes, a
// name of 1 to 64 bytes and a value of at most 1024; more fails the
// connection, SQLCODE -919 and SQLSTATE 08006, before it is sent.
func TestAttributesBounded(t *testing.T) {
	t.Parallel()

	most := make([]Attribute, maxAttributes)
	for i := range most {
		most[i] = Attribute{Name: strings.Repeat("n", maxAttributeName), Value: strings.Repeat("v", maxAttributeValue)}
	}

	if err := checkAttributes(most); err != nil {
		t.Errorf("32 attributes at their bounds: %v", err)
	}

	cases := map[string][]Attribute{
		"33":         append(append([]Attribute{}, most...), Attribute{Name: "x"}),
		"empty name": {{Name: ""}},
		"long name":  {{Name: strings.Repeat("n", maxAttributeName+1)}},
		"long value": {{Name: "n", Value: strings.Repeat("v", maxAttributeValue+1)}},
	}
	for name, attributes := range cases {
		err := checkAttributes(attributes)
		if err == nil {
			t.Errorf("%s: taken", name)

			continue
		}

		assertFailure(t, err, sqlcodeBeyond, sqlstateFeatureNotSupported, "attribute")
	}
}
