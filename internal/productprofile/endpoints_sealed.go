//go:build product_production || product_test

package productprofile

// Sealed binaries have no developer linker inputs and keep the checked-in origin.
func loadBuildProfile(profileJSON, endpointsJSON string) (Profile, error) {
	return loadProfile(profileJSON, endpointsJSON)
}
