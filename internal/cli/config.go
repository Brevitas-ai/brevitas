package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// cmdConfig prints or edits Brevitas's own configuration.
//
//	brevitas config                     # print current config + path
//	brevitas config set-port 8081       # change the proxy port
//	brevitas config set-upstream openai https://api.openai.com
//	brevitas config set-python python3.12
func (a *App) cmdConfig(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.printConfig()
	}

	switch args[0] {
	case "path":
		a.say("%s", a.Dirs.ConfigFile())
		return nil

	case "set-port":
		if len(args) != 2 {
			return fmt.Errorf("usage: bvx config set-port <port>")
		}
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid port %q", args[1])
		}
		a.Cfg.Proxy.Port = port
		if err := a.Cfg.Save(); err != nil {
			return err
		}
		a.ok("proxy port set to %d (run 'bvx repair' to re-point tools)", port)
		return nil

	case "set-upstream":
		if len(args) != 3 {
			return fmt.Errorf("usage: bvx config set-upstream <family> <url>")
		}
		if a.Cfg.Upstreams == nil {
			a.Cfg.Upstreams = map[string]string{}
		}
		a.Cfg.Upstreams[args[1]] = args[2]
		if err := a.Cfg.Save(); err != nil {
			return err
		}
		a.ok("upstream %s -> %s", args[1], args[2])
		return nil

	case "set-python":
		if len(args) != 2 {
			return fmt.Errorf("usage: bvx config set-python <interpreter>")
		}
		a.Cfg.Optimizer.PythonBin = args[1]
		if err := a.Cfg.Save(); err != nil {
			return err
		}
		a.ok("python interpreter set to %s", args[1])
		return nil

	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func (a *App) printConfig() error {
	data, err := json.MarshalIndent(a.Cfg, "", "  ")
	if err != nil {
		return err
	}
	a.say("# %s", a.Dirs.ConfigFile())
	a.say("%s", string(data))
	return nil
}
