package process

func AfterPTYStartHookGet() func() {
	return afterPTYStartHook
}

func AfterPTYStartHookSet(next func()) {
	afterPTYStartHook = next
}
