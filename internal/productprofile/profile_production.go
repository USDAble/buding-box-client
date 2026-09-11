//go:build product_production

package productprofile

import _ "embed"

//go:embed profiles/production.json
var embeddedProfileJSON string
