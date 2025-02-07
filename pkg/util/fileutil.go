package util

import (
	"os"
)

func WriteFileAtomic(filePath string, d []byte) error {
	f, err := os.CreateTemp(os.TempDir(), AlphabetsLowerCase(10))
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(d)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filePath); err != nil {
		return err
	}
	return nil
}

func CheckPathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
