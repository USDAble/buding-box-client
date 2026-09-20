//go:build product_test && !product_production

package productprofile

import _ "embed"

//go:embed profiles/testing.json
var embeddedProfileJSON string
