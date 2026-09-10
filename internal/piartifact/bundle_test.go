package piartifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func fixtureBundle(t *testing.T) Bundle {
	t.Helper()
	inner := []byte("inner package fixture")
	source := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sum := sha256.Sum256(inner)
	return Bundle{
		Inner:      inner,
		Provenance: []byte(`{"name":"@vgxness/pi","version":"0.1.0","sourceSHA256":"` + source + `","provenance":"local source snapshot; unpublished"}`),
		Checksums:  []byte(hex.EncodeToString(sum[:]) + "  " + innerName() + "\n"),
		Release: Release{
			ReleaseVersion: "v1.2.3",
			Commit:         "0123456789abcdef0123456789abcdef01234567",
			PackageVersion: PackageVersion,
			SourceSHA256:   source,
		},
	}
}

func TestBundleRoundTrip(t *testing.T) {
	bundle := fixtureBundle(t)
	encoded, err := Encode(bundle)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Inner, bundle.Inner) || !bytes.Equal(decoded.Provenance, bundle.Provenance) || !bytes.Equal(decoded.Checksums, bundle.Checksums) || decoded.Release != bundle.Release {
		t.Fatalf("round trip mismatch: %#v", decoded)
	}
	name, err := Filename(bundle.Release.ReleaseVersion)
	if err != nil || name != "vgxness-pi_1.2.3_portable.tar.gz" {
		t.Fatalf("filename = %q, %v", name, err)
	}
}

func TestBundleRejectsInvalidMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*Bundle){
		"non v semver":                    func(b *Bundle) { b.Release.ReleaseVersion = "1.2.3" },
		"leading zero semver":             func(b *Bundle) { b.Release.ReleaseVersion = "v01.2.3" },
		"numeric prerelease leading zero": func(b *Bundle) { b.Release.ReleaseVersion = "v1.2.3-01" },
		"empty prerelease segment":        func(b *Bundle) { b.Release.ReleaseVersion = "v1.2.3-a..b" },
		"empty build segment":             func(b *Bundle) { b.Release.ReleaseVersion = "v1.2.3+a..b" },
		"uppercase commit":                func(b *Bundle) { b.Release.Commit = "0123456789ABCDEF0123456789abcdef01234567" },
		"short source":                    func(b *Bundle) { b.Release.SourceSHA256 = "abcd" },
		"mismatched provenance": func(b *Bundle) {
			b.Provenance = []byte(`{"name":"@vgxness/pi","version":"0.1.0","sourceSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","provenance":"local source snapshot; unpublished"}`)
		},
		"extra provenance": func(b *Bundle) {
			b.Provenance = append(bytes.TrimSuffix(b.Provenance, []byte("}")), []byte(`,"extra":"x"}`)...)
		},
		"duplicate provenance": func(b *Bundle) {
			b.Provenance = []byte(`{"name":"@vgxness/pi","name":"@vgxness/pi","version":"0.1.0","sourceSHA256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","provenance":"local source snapshot; unpublished"}`)
		},
		"checksum mismatch": func(b *Bundle) {
			b.Checksums = []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  " + innerName() + "\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			bundle := fixtureBundle(t)
			mutate(&bundle)
			if _, err := Encode(bundle); err == nil {
				t.Fatal("Encode accepted invalid bundle")
			}
		})
	}
}

func TestBundleDecodeRejectsHostileArchives(t *testing.T) {
	valid := fixtureBundle(t)
	release := []byte(`{"releaseVersion":"v1.2.3","commit":"0123456789abcdef0123456789abcdef01234567","packageVersion":"0.1.0","sourceSHA256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`)
	members := map[string][]byte{innerName(): valid.Inner, "PROVENANCE.json": valid.Provenance, "SHA256SUMS": valid.Checksums, "RELEASE.json": release}
	cases := map[string]func() []tarMember{
		"unknown member": func() []tarMember {
			return append(membersToSlice(members), tarMember{name: "extra", data: []byte("x")})
		},
		"traversal member": func() []tarMember {
			return append(membersToSlice(members), tarMember{name: "../RELEASE.json", data: release})
		},
		"duplicate member": func() []tarMember {
			base := membersToSlice(members)
			return append(base, tarMember{name: "RELEASE.json", data: release})
		},
		"symlink": func() []tarMember {
			base := membersToSlice(members)
			base[0].typeflag = tar.TypeSymlink
			base[0].link = "target"
			return base
		},
		"hardlink": func() []tarMember {
			base := membersToSlice(members)
			base[0].typeflag = tar.TypeLink
			base[0].link = "target"
			return base
		},
		"missing member": func() []tarMember { base := membersToSlice(members); return base[:3] },
		"unknown release key": func() []tarMember {
			base := membersToSlice(members)
			for i := range base {
				if base[i].name == "RELEASE.json" {
					base[i].data = append(bytes.TrimSuffix(release, []byte("}")), []byte(`,"extra":"x"}`)...)
				}
			}
			return base
		},
		"duplicate release key": func() []tarMember {
			base := membersToSlice(members)
			for i := range base {
				if base[i].name == "RELEASE.json" {
					base[i].data = []byte(`{"releaseVersion":"v1.2.3","releaseVersion":"v1.2.3","commit":"0123456789abcdef0123456789abcdef01234567","packageVersion":"0.1.0","sourceSHA256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`)
				}
			}
			return base
		},
		"oversized metadata": func() []tarMember {
			base := membersToSlice(members)
			for i := range base {
				if base[i].name == "RELEASE.json" {
					base[i].data = bytes.Repeat([]byte("x"), maxMetadata+1)
				}
			}
			return base
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(archiveBytes(t, build())); err == nil {
				t.Fatal("Decode accepted hostile archive")
			}
		})
	}
	encoded, err := Encode(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded[:len(encoded)-1]); err == nil {
		t.Fatal("Decode accepted truncated gzip")
	}
	if _, err := Decode(append(encoded, []byte("trailing")...)); err == nil {
		t.Fatal("Decode accepted trailing gzip data")
	}
	if _, err := Decode(make([]byte, maxCompressed+1)); err == nil {
		t.Fatal("Decode accepted compressed oversize")
	}
	if _, err := Decode(archiveWithTrailing(t, membersToSlice(members))); err == nil {
		t.Fatal("Decode accepted trailing decompressed data")
	}
}

type tarMember struct {
	name     string
	data     []byte
	typeflag byte
	link     string
}

func membersToSlice(members map[string][]byte) []tarMember {
	return []tarMember{{innerName(), members[innerName()], tar.TypeReg, ""}, {"PROVENANCE.json", members["PROVENANCE.json"], tar.TypeReg, ""}, {"SHA256SUMS", members["SHA256SUMS"], tar.TypeReg, ""}, {"RELEASE.json", members["RELEASE.json"], tar.TypeReg, ""}}
}

func archiveBytes(t *testing.T, members []tarMember) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, member := range members {
		typeflag := member.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(&tar.Header{Name: member.name, Mode: 0o644, Size: int64(len(member.data)), Typeflag: typeflag, Linkname: member.link}); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tw.Write(member.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func archiveWithTrailing(t *testing.T, members []tarMember) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, member := range members {
		if err := tw.WriteHeader(&tar.Header{Name: member.name, Mode: 0o644, Size: int64(len(member.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gz.Write([]byte("trailing tar bytes")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
