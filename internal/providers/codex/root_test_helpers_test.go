package codex

import "os"

func testWrite(r *Root, name string, data []byte) error {
	f, err := r.fs.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if close := f.Close(); err == nil {
		err = close
	}
	if err != nil {
		return err
	}
	return r.sync(".")
}
