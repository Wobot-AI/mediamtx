package conf

import (
	"strings"
)

const (
	redactedCredential = "<redacted>"
)

// redactHeaders replaces the value of every header with a placeholder.
// All values are redacted, since allow-listing by header name would leak
// any credential passed under an unexpected name.
func redactHeaders(headers []string) {
	for i, h := range headers {
		if key, _, ok := strings.Cut(h, ":"); ok {
			headers[i] = key + ": " + redactedCredential
		}
	}
}

func redactPathHeaders(pathConf *Path) {
	redactHeaders(pathConf.RunOnDemandHTTPHeaders)
	redactHeaders(pathConf.RunOnUnDemandHTTPHeaders)
}

// Redact clones a configuration and redacts credentials from it.
func Redact(c *Conf) *Conf {
	c = c.Clone()

	for i := range c.AuthInternalUsers {
		if c.AuthInternalUsers[i].Pass != "" {
			c.AuthInternalUsers[i].Pass = Credential(redactedCredential)
		}
	}

	if c.PathDefaults.PublishPass != nil && *c.PathDefaults.PublishPass != "" {
		*c.PathDefaults.PublishPass = Credential(redactedCredential)
	}
	if c.PathDefaults.ReadPass != nil && *c.PathDefaults.ReadPass != "" {
		*c.PathDefaults.ReadPass = Credential(redactedCredential)
	}
	redactPathHeaders(&c.PathDefaults)

	for _, pathConf := range c.Paths {
		redactPathHeaders(pathConf)

		if pathConf.PublishPass != nil && *pathConf.PublishPass != "" {
			*pathConf.PublishPass = Credential(redactedCredential)
		}
		if pathConf.ReadPass != nil && *pathConf.ReadPass != "" {
			*pathConf.ReadPass = Credential(redactedCredential)
		}
	}

	return c
}
