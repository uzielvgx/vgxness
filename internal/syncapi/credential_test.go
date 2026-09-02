package syncapi

import "testing"

const canonicalTestBearer = "vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestParseBearerAcceptsOnlyCanonicalCredential(t *testing.T) {
	credential, ok := ParseBearer(canonicalTestBearer)
	if !ok || credential.DeviceID.String() != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("ParseBearer canonical = %+v, %t", credential, ok)
	}

	for _, bearer := range []string{
		"vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"vgx1.123E4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"vgx1.00000000-0000-0000-0000-000000000000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"vgx1.123e4567-e89b-12d3-a456-426614174000.!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!",
	} {
		if credential, ok := ParseBearer(bearer); ok {
			t.Fatalf("ParseBearer accepted %q: %+v", bearer, credential)
		}
	}
}
