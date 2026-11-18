package gorules

// swallowedErrFixtures pins the rule that catches the credits settle bug (see
// SwallowedErrorInCheck). Both arms matter: the discard is only a bug when it
// is the whole branch body, so the "handled below" case must stay legal or the
// rule would be unusable.
func swallowedErrFixtures() []ruleFixture {
	return []ruleFixture{
		{
			name: "error discarded inside its own nil-check is flagged",
			source: goFile(`func swallowedErrf() {
	err := swallowedErrfWork()
	if err != nil {
		_ = err
	}
}

func swallowedErrfWork() error { return nil }`),
			wantAtLeast: 1,
		},
		{
			name: "the if-with-init form is flagged too",
			source: goFile(`func swallowedErrInit() {
	if err := swallowedErrInitWork(); err != nil {
		_ = err
	}
}

func swallowedErrInitWork() error { return nil }`),
			wantAtLeast: 1,
		},
		{
			name: "an error logged after the check stays quiet",
			source: goFile(`func swallowedErrHandled() {
	err := swallowedErrHandledWork()
	if err != nil {
		swallowedErrHandledSink(err)
	}
}

func swallowedErrHandledSink(error)      {}
func swallowedErrHandledWork() error     { return nil }`),
			wantAtLeast: 0,
		},
		{
			name: "a deliberate discard at the call site stays quiet",
			source: goFile(`func swallowedErrIntentional() {
	_ = swallowedErrIntentionalWork()
}

func swallowedErrIntentionalWork() error { return nil }`),
			wantAtLeast: 0,
		},
	}
}
