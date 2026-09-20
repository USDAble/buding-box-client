//go:build !product_production && !product_test

package productprofile

import _ "embed"

//go:embed profiles/developer.json
var embeddedProfileJSON string
