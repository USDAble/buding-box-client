package productstate

// SetModelReasoning records the gateway model's preference independently of
// upstream global configuration and independently of reasoning visibility.
func (s *Store) SetModelReasoning(model, effort string) error {
	return s.mutate(func(st *State) {
		if effort == "default" {
			delete(st.Prefs.ModelReasoning, model)
			return
		}
		if st.Prefs.ModelReasoning == nil {
			st.Prefs.ModelReasoning = map[string]string{}
		}
		st.Prefs.ModelReasoning[model] = effort
	})
}
