package customobject

// PostgreSQLConfigList represents a list of custom objects. It is useful for
// decoding list API calls.
type PostgreSQLConfigList struct {
	Items []*PostgreSQLConfig `json:"items"`
}
