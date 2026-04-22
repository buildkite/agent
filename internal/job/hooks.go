package job

import "context"

func (e *Executor) executeHooksForward(ctx context.Context, hookName string) error {
	if err := e.executeGlobalHook(ctx, hookName); err != nil {
		return err
	}
	if err := e.executeLocalHook(ctx, hookName); err != nil {
		return err
	}
	return e.executePluginHook(ctx, hookName, e.pluginCheckouts)
}

func (e *Executor) executeHooksReverse(ctx context.Context, hookName string) error {
	if err := e.executePluginHook(ctx, hookName, e.pluginCheckoutsReversed); err != nil {
		return err
	}
	if err := e.executeLocalHook(ctx, hookName); err != nil {
		return err
	}
	return e.executeGlobalHook(ctx, hookName)
}
