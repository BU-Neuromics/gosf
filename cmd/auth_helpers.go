package cmd

import (
	"errors"
	"fmt"

	"github.com/BU-Neuromics/gosf/internal/client"
)

// friendlyAuthError converts a raw 401/403 from a read command (ls/info/pull/
// status/versions) into an actionable message. These commands attempt the fetch
// unauthenticated first — public projects need no token — so a 401/403 means the
// resource is private and a token is required.
func friendlyAuthError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
		return fmt.Errorf("access denied (HTTP %d) — this project is private or the token is invalid; "+
			"run 'gosf auth login' or set OSF_TOKEN to authenticate", apiErr.StatusCode)
	}
	return err
}
