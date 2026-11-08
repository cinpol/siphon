package goceph

import (
	"fmt"
	"strings"
)

// monError wraps a failed mon command's error with Ceph's own explanation.
//
// librados reports only the errno (e.g. "rados: ret=-22, Invalid argument");
// the reason (e.g. "Not enough remaining RGW daemons. Use --force") comes back
// separately as the command's status string. Without it the operator sees just
// "Invalid argument". The errno stays wrapped, so errors.Is and the service
// layer's permission-hint check keep working.
func monError(prefix any, status string, err error) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("mon command %v: %w", prefix, err)
	}
	return fmt.Errorf("mon command %v: %s (%w)", prefix, status, err)
}
