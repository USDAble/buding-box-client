//go:build !product_production

package productprofile

import _ "embed"

//go:embed profiles/developer.json
var embeddedProfileJSON string
