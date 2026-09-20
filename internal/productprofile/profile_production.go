//go:build product_production && !product_test

package productprofile

import _ "embed"

//go:embed profiles/production.json
var embeddedProfileJSON string
