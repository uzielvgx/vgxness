package syncapi

import (
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
)

// Bearer is the non-secret identity encoded in a canonical sync credential.
type Bearer struct {
	DeviceID uuid.UUID
}

// ParseBearer accepts only the canonical vgx1 bearer wire format.
func ParseBearer(value string) (Bearer, bool) {
	if len(value) != 85 {
		return Bearer{}, false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != "vgx1" {
		return Bearer{}, false
	}
	deviceID, err := uuid.Parse(parts[1])
	if err != nil || deviceID == uuid.Nil || deviceID.String() != parts[1] {
		return Bearer{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	defer clearBytes(raw)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != parts[2] {
		return Bearer{}, false
	}
	return Bearer{DeviceID: deviceID}, true
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
