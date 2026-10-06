// Package datasets holds the small embedded dictionaries used by the name and
// company transformers.
//
// The lists are part of the v1 algorithm: outputs are selected by index, so
// adding, removing or reordering a single entry changes fake identities.
// They are frozen; a test pins their checksums.
package datasets

import (
	_ "embed"
	"strings"
)

var (
	//go:embed first_names.txt
	firstNames string
	//go:embed last_names.txt
	lastNames string
	//go:embed company_names.txt
	companyNames string
	//go:embed company_suffixes.txt
	companySuffixes string

	FirstNames      = strings.Fields(firstNames)
	LastNames       = strings.Fields(lastNames)
	CompanyNames    = strings.Fields(companyNames)
	CompanySuffixes = strings.Fields(companySuffixes)
)
