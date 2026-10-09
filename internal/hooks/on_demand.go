package hooks

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/bluenviron/mediamtx/internal/conf"
	"github.com/bluenviron/mediamtx/internal/externalcmd"
	"github.com/bluenviron/mediamtx/internal/httphook"
	"github.com/bluenviron/mediamtx/internal/logger"
)

// OnDemandParams are the parameters of OnDemand.
type OnDemandParams struct {
	Logger          logger.Writer
	ExternalCmdPool *externalcmd.Pool
	HTTPHookPool    *httphook.Pool
	Conf            *conf.Path
	ExternalCmdEnv  externalcmd.Environment
	Query           string
}

// demandEpisode records the outcome of a demand request, so that the matching
// un-demand request can decide whether it is needed at all.
type demandEpisode struct {
	mutex   sync.Mutex
	started bool
	outcome httphook.Outcome
}

func (e *demandEpisode) setOutcome(o httphook.Outcome) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.started = true
	e.outcome = o
}

// needsUnDemand reports whether the un-demand request must be performed.
// It is skipped only when the demand request was definitively refused, since in
// that case the remote side is known not to have started. An unknown outcome
// always leads to an un-demand request.
func (e *demandEpisode) needsUnDemand() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return !e.started || e.outcome != httphook.OutcomeRejected
}

// OnDemand is the OnDemand hook.
func OnDemand(params OnDemandParams) func(string) {
	var env externalcmd.Environment
	var onDemandCmd *externalcmd.Cmd

	hasHTTP := params.Conf.RunOnDemandHTTPAddress != "" || params.Conf.RunOnUnDemandHTTPAddress != ""

	if params.Conf.RunOnDemand != "" || params.Conf.RunOnUnDemand != "" || hasHTTP {
		env = params.ExternalCmdEnv
		env["MTX_QUERY"] = url.QueryEscape(params.Query)
	}

	if params.Conf.RunOnDemand != "" {
		params.Logger.Log(logger.Info, "runOnDemand command started")

		onDemandCmd = &externalcmd.Cmd{
			Pool:    params.ExternalCmdPool,
			Cmdstr:  params.Conf.RunOnDemand,
			Restart: params.Conf.RunOnDemandRestart,
			Env:     env,
			OnExit: func(err error) {
				params.Logger.Log(logger.Info, "runOnDemand command exited: %v", err)
			},
		}
		onDemandCmd.Start()
	}

	var episode *demandEpisode
	var hookKey string

	if hasHTTP {
		episode = &demandEpisode{}

		hookKey = externalcmd.ExpandEnv(params.Conf.RunOnDemandHTTPKey, env)
		if hookKey == "" {
			hookKey = env["MTX_PATH"]
		}

		if params.Conf.RunOnDemandHTTPAddress != "" {
			params.Logger.Log(logger.Info, "runOnDemandHTTP request launched")

			req := &httphook.Request{
				Address:       externalcmd.ExpandEnv(params.Conf.RunOnDemandHTTPAddress, env),
				Headers:       expandAll(params.Conf.RunOnDemandHTTPHeaders, env),
				Body:          externalcmd.ExpandEnv(params.Conf.RunOnDemandHTTPBody, env),
				Timeout:       time.Duration(params.Conf.RunOnDemandHTTPTimeout),
				Retries:       params.Conf.RunOnDemandHTTPRetries,
				RetryInterval: time.Duration(params.Conf.RunOnDemandHTTPRetryInterval),
				// retrying past the moment readers give up is pure latency.
				Deadline: time.Now().Add(time.Duration(params.Conf.RunOnDemandStartTimeout)),
				Logger:   params.Logger,
				Desc:     "runOnDemandHTTP",
			}

			params.HTTPHookPool.Enqueue(hookKey, func(ctx context.Context) {
				episode.setOutcome(req.Do(ctx))
			})
		}
	}

	return func(reason string) {
		if onDemandCmd != nil {
			onDemandCmd.Close()
			params.Logger.Log(logger.Info, "runOnDemand command stopped: %v", reason)
		}

		if params.Conf.RunOnUnDemand != "" {
			params.Logger.Log(logger.Info, "runOnUnDemand command launched")
			cmd := &externalcmd.Cmd{
				Pool:    params.ExternalCmdPool,
				Cmdstr:  params.Conf.RunOnUnDemand,
				Restart: false,
				Env:     env,
			}
			cmd.Start()
		}

		if !hasHTTP {
			return
		}

		address := params.Conf.RunOnUnDemandHTTPAddress
		if address == "" {
			address = params.Conf.RunOnDemandHTTPAddress
		}
		if address == "" {
			return
		}

		// an empty list, not just a missing one, falls back: the reference
		// configuration ships these fields as [].
		headers := params.Conf.RunOnUnDemandHTTPHeaders
		if len(headers) == 0 {
			headers = params.Conf.RunOnDemandHTTPHeaders
		}

		req := &httphook.Request{
			Address:       externalcmd.ExpandEnv(address, env),
			Headers:       expandAll(headers, env),
			Body:          externalcmd.ExpandEnv(params.Conf.RunOnUnDemandHTTPBody, env),
			Timeout:       time.Duration(params.Conf.RunOnDemandHTTPTimeout),
			Retries:       params.Conf.RunOnDemandHTTPRetries,
			RetryInterval: time.Duration(params.Conf.RunOnDemandHTTPRetryInterval),
			Logger:        params.Logger,
			Desc:          "runOnUnDemandHTTP",
		}

		params.Logger.Log(logger.Info, "runOnUnDemandHTTP request launched: %v", reason)

		// the pool cancels and waits for the demand request before running this,
		// so the episode outcome is final by the time it is read.
		params.HTTPHookPool.Enqueue(hookKey, func(ctx context.Context) {
			if !episode.needsUnDemand() {
				params.Logger.Log(logger.Debug,
					"runOnUnDemandHTTP skipped: the demand request was refused")
				return
			}
			req.Do(ctx)
		})
	}
}

func expandAll(entries []string, env externalcmd.Environment) []string {
	if entries == nil {
		return nil
	}
	ret := make([]string, len(entries))
	for i, e := range entries {
		ret[i] = externalcmd.ExpandEnv(e, env)
	}
	return ret
}
