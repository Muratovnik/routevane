package discovery

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// RunScenario performs one guided learning session: an ordered, repeatable set
// of exploration steps in a single isolated browser profile.
//
// It shares the whole boundary with the single-page load — the same guarded
// proxy, the same destination policy, the same bounds, and the same temporary
// profile that is removed on success, failure, and cancellation. The only
// difference is that each step names the component its observations belong to,
// which is what allows a media or voice dependency to be accepted only after the
// corresponding action actually ran.
func RunScenario(ctx context.Context, scenario Scenario, options BrowserOptions) (evidence SessionEvidence, err error) {
	if ctx == nil {
		return SessionEvidence{}, ErrInvalidComposition
	}
	if err := scenario.Validate(); err != nil {
		return SessionEvidence{}, err
	}
	target, err := NormalizeTarget(scenario.Target)
	if err != nil {
		return SessionEvidence{}, err
	}
	execPath, err := resolveBrowserExecutable(options.ExecPath)
	if err != nil {
		return SessionEvidence{}, err
	}
	options.ExecPath = execPath
	options = options.withDefaults()
	if options.Timeout < scenario.minimumTimeout() {
		options.Timeout = scenario.minimumTimeout()
	}

	profileDir, err := os.MkdirTemp(options.UserDataParent, "routevane-session-")
	if err != nil {
		return SessionEvidence{}, fmt.Errorf("create session profile: %w", err)
	}
	// The profile is removed on every path, including a panic in the browser
	// driver, so a cancelled session cannot leave browsing state on disk. A
	// removal failure is reported on the evidence rather than dropped: it never
	// overwrites the operation's own error.
	defer func() {
		if cleanupErr := removeProfileDir(profileDir); cleanupErr != nil {
			evidence.CleanupError = cleanupErr.Error()
		}
	}()

	sessionCtx, cancelSession := context.WithTimeout(ctx, options.Timeout)
	defer cancelSession()

	proxy := newGuardedProxy(options.Resolver, options.Dialer, options.MaxRequests, options.MaxHosts, options.MaxBytes)
	proxyAddress, stopProxy, err := proxy.listenLoopback(sessionCtx)
	if err != nil {
		return SessionEvidence{}, err
	}
	defer stopProxy()

	allocatorCtx, cancelAllocator, err := newBrowserAllocator(sessionCtx, options, profileDir, proxyAddress)
	if err != nil {
		return SessionEvidence{}, err
	}
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	var runErr error
	for _, step := range scenario.Steps {
		documentHost := ""
		actions := make([]chromedp.Action, 0, 2)
		if step.URL != "" {
			stepTarget, targetErr := NormalizeTarget(step.URL)
			if targetErr != nil {
				runErr = targetErr
				break
			}
			documentHost = stepTarget.Host
			actions = append(actions, chromedp.Navigate(stepTarget.URL))
		}
		settle := time.Duration(step.SettleSeconds) * time.Second
		if step.SettleSeconds == 0 {
			settle = DefaultStepSettle * time.Second
		}
		actions = append(actions, chromedp.Sleep(settle))
		// The step is announced before its actions run, so a request the step
		// causes cannot be attributed to the previous step.
		proxy.beginStep(step.ID, step.Component, documentHost)
		if err := chromedp.Run(browserCtx, actions...); err != nil {
			runErr = fmt.Errorf("%w: step %q: %v", ErrPageLoadFailed, step.ID, err)
			break
		}
	}
	evidence = proxy.evidence(target)
	if runErr != nil {
		return evidence, runErr
	}
	return evidence, nil
}

func (s Scenario) minimumTimeout() time.Duration {
	total := 10 * time.Second
	for _, step := range s.Steps {
		settle := step.SettleSeconds
		if settle == 0 {
			settle = DefaultStepSettle
		}
		total += time.Duration(settle)*time.Second + 10*time.Second
	}
	return total
}
