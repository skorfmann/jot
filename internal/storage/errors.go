package storage

import (
	"errors"
	"fmt"
	"strings"

	"gocloud.dev/gcerrors"
)

func IsPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	if gcerrors.Code(err) == gcerrors.FailedPrecondition {
		return true
	}
	return strings.Contains(err.Error(), "PreconditionFailed") || strings.Contains(err.Error(), "status code: 412")
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrNotFound) || gcerrors.Code(err) == gcerrors.NotFound
}

func FormatLimit(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
