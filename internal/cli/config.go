package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	configDirName  = "grus"
	configFileName = "config.yaml"
)

// fileContext is one named controller connection in the config file. It stores the path of a token
// file, never the token itself.
type fileContext struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	TokenFile string `yaml:"token-file,omitempty"`
}

// fileConfig is the content of ~/.config/grus/config.yaml.
type fileConfig struct {
	Current  string        `yaml:"current-context,omitempty"`
	Contexts []fileContext `yaml:"contexts,omitempty"`
}

func (c *fileConfig) find(name string) *fileContext {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

func (c *fileConfig) set(ctx fileContext) {
	if existing := c.find(ctx.Name); existing != nil {
		*existing = ctx
		return
	}
	c.Contexts = append(c.Contexts, ctx)
}

func (e *Env) configDir() (string, error) {
	base, err := e.ConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the user config directory: %w", err)
	}
	return filepath.Join(base, configDirName), nil
}

// loadConfig reads the config file; a missing file is an empty config.
func (e *Env) loadConfig() (fileConfig, error) {
	dir, err := e.configDir()
	if err != nil {
		return fileConfig{}, nil //nolint:nilerr // no config dir means no saved contexts; flags and env still work
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return fileConfig{}, nil
	}
	if err != nil {
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("parse %s: %w", filepath.Join(dir, configFileName), err)
	}
	return cfg, nil
}

func (e *Env) saveConfig(cfg fileConfig) error {
	dir, err := e.configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, configFileName), raw, 0o600)
}

// connection is the resolved way to reach a controller.
type connection struct {
	Context string
	URL     string
	Token   string
}

// resolve combines flags, environment and the config file. Precedence for the URL: --url, GRUS_URL, context.
// Token: GRUS_TOKEN, --token-file, token file of the context.
func (e *Env) resolve(o *opts) (connection, error) {
	cfg, err := e.loadConfig()
	if err != nil {
		return connection{}, err
	}
	name := o.context
	if name == "" {
		name = cfg.Current
	}
	var entry fileContext
	if name != "" {
		found := cfg.find(name)
		if found == nil {
			return connection{}, fmt.Errorf("unknown context %q, see grus config list", name)
		}
		entry = *found
	}
	conn := connection{Context: name, URL: firstNonEmpty(o.url, e.Getenv("GRUS_URL"), entry.URL)}
	if conn.URL == "" {
		return connection{}, errors.New("no controller URL: run grus login --url <url>, pass --url or set GRUS_URL")
	}
	conn.Token = strings.TrimSpace(e.Getenv("GRUS_TOKEN"))
	if conn.Token == "" {
		if file := firstNonEmpty(o.tokenFile, entry.TokenFile); file != "" {
			raw, err := os.ReadFile(file)
			if err != nil {
				return connection{}, fmt.Errorf("read token file: %w", err)
			}
			conn.Token = strings.TrimSpace(string(raw))
		}
	}
	return conn, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func newLoginCmd(env *Env) *cobra.Command {
	var name, url, tokenFile string
	var tokenStdin bool
	cmd := &cobra.Command{
		Use:   "login --url <url> [--name <context>] [--token-file <path> | --token-stdin]",
		Short: "Save a controller connection as a context and make it current",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if url == "" {
				return errors.New("--url is required")
			}
			if tokenFile != "" && tokenStdin {
				return errors.New("use either --token-file or --token-stdin")
			}
			dir, err := env.configDir()
			if err != nil {
				return err
			}
			if tokenStdin {
				raw, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				token := strings.TrimSpace(string(raw))
				if token == "" {
					return errors.New("no token on stdin")
				}
				tokenFile = filepath.Join(dir, "tokens", name)
				if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
					return err
				}
				if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
					return err
				}
			} else if tokenFile != "" {
				if tokenFile, err = filepath.Abs(tokenFile); err != nil {
					return err
				}
			}
			cfg, err := env.loadConfig()
			if err != nil {
				return err
			}
			cfg.set(fileContext{Name: name, URL: strings.TrimRight(url, "/"), TokenFile: tokenFile})
			cfg.Current = name
			if err := env.saveConfig(cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "context %q saved and selected\n", name)
			return err
		},
	}
	cmd.Flags().StringVar(&name, "name", "default", "context name")
	cmd.Flags().StringVar(&url, "url", "", "controller base URL")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "file holding the API token")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the token from stdin and store it in a private file")
	return cmd
}

func newConfigCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage saved contexts"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List the saved contexts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := env.loadConfig()
			if err != nil {
				return err
			}
			items := make([]map[string]any, 0, len(cfg.Contexts))
			for _, c := range cfg.Contexts {
				current := ""
				if c.Name == cfg.Current {
					current = "*"
				}
				items = append(items, map[string]any{"name": c.Name, "url": c.URL, "tokenFile": c.TokenFile, "current": current})
			}
			return printList(cmd, outputFormat(cmd), items, []column{
				{"CURRENT", field("current")}, {"NAME", field("name")}, {"URL", field("url")}, {"TOKEN-FILE", field("tokenFile")},
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "current", Short: "Print the current context name", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := env.loadConfig()
			if err != nil {
				return err
			}
			if cfg.Current == "" {
				return errors.New("no current context, run grus login")
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), cfg.Current)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "use <context>", Short: "Select the current context", Args: cobra.ExactArgs(1),
		ValidArgsFunction: completeContexts(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := env.loadConfig()
			if err != nil {
				return err
			}
			if cfg.find(args[0]) == nil {
				return fmt.Errorf("unknown context %q", args[0])
			}
			cfg.Current = args[0]
			if err := env.saveConfig(cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "current context is %q\n", args[0])
			return err
		},
	})
	return cmd
}
