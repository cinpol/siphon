package goceph

// Config holds the connection parameters for the native go-ceph client.
//
// It is defined outside any build tag so that callers (cmd/siphon) compile
// identically whether or not the go-ceph transport is built in. The real and
// stub implementations of New both accept this type.
type Config struct {
	// ConfigPath is the path to ceph.conf. Empty means "use librados defaults"
	// (i.e. the standard search path, matching the ceph CLI's behaviour).
	ConfigPath string

	// User is the Ceph client user, e.g. "client.admin".
	User string
}
