//go:build !windows && !darwin && !linux

package secret

func setPlatform(_, _ string) error         { return ErrUnsupportedPlatform }
func getPlatform(_ string) (string, error)  { return "", ErrUnsupportedPlatform }
func deletePlatform(_ string) error         { return ErrUnsupportedPlatform }
