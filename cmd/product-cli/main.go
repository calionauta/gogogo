// Command product-cli is the CONSUMER CLI surface of a gogogo product.
//
// The scriptable half a product's user runs, alongside `cmd/product-mcp`: same
// engine (`internal/productapi`), same session, same policy, different
// transport. A shell script, a cron job or an agent without MCP can drive the
// product with it; an agent that speaks MCP uses the sibling binary instead.
// Neither is more complete — both reach the product's own routes.
//
// It links internal/productapi and nothing else: no installer, no capabilities
// registry, no shape tooling. That is what lets a product ship it while the
// builder (`cmd/gogogo`, `cmd/gogogo-mcp`) stays on the developer's machine.
//
// The template's `cmd/gogogo` is the mirror image: it evolves a checkout. This
// runs a deployed product.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/calionauta/gogogo/internal/productapi"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "product: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("a command is required")
	}
	cmd, rest := args[0], args[1:]

	fs := flag.NewFlagSet("product "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", firstNonEmpty(os.Getenv("GOGOGO_SERVER"), "http://127.0.0.1:8080"),
		"base URL of the running product (loopback only)")
	session := fs.String("session", os.Getenv("GOGOGO_SESSION"),
		"product session token (or GOGOGO_SESSION); never a password")
	asJSON := fs.Bool("json", false, "print the answer as JSON")

	switch cmd {
	case "describe", "list":
		if err := fs.Parse(rest); err != nil {
			return err
		}
		client := productapi.Client{BaseURL: *server, Session: *session}
		units, err := client.Descriptors(ctx)
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(stdout, units)
		}
		printUnits(stdout, units)
		return nil

	case "invoke", "call":
		// Client flags are parsed FIRST, then the two positionals and their
		// `--param` pairs. Parsing positionals first made `--server` look like
		// a verb param and fail with the wrong message.
		id, verb, params, err := parseInvoke(splitClientFlags(rest, fs))
		if err != nil {
			return err
		}
		client := productapi.Client{BaseURL: *server, Session: *session}
		body, err := client.Invoke(ctx, id, verb, params)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, body)
		return nil

	case "help", "-h", "--help":
		usage(stdout)
		return nil

	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// splitClientFlags hands the client's own flags to fs and returns everything
// else, so `--server`/`--session`/`--json` never reach parseInvoke.
func splitClientFlags(args []string, fs *flag.FlagSet) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--server" || arg == "-server",
			arg == "--session" || arg == "-session":
			if i+1 < len(args) {
				_ = fs.Set(strings.TrimLeft(arg, "-"), args[i+1])
				i++
			}
		case strings.HasPrefix(arg, "--server="), strings.HasPrefix(arg, "-server="):
			_ = fs.Set("server", strings.SplitN(arg, "=", 2)[1])
		case strings.HasPrefix(arg, "--session="), strings.HasPrefix(arg, "-session="):
			_ = fs.Set("session", strings.SplitN(arg, "=", 2)[1])
		case arg == "--json" || arg == "-json":
			_ = fs.Set("json", "true")
		default:
			rest = append(rest, arg)
		}
	}
	return rest
}

// parseInvoke reads `invoke <unit> <verb> [--param k=v]...`. Params come after
// the two positional arguments so a verb's own flags cannot be mistaken for
// the client's (`--server` belongs to the client, `--param` to the verb).
func parseInvoke(args []string) (id, verb string, params map[string]string, err error) {
	var positional []string
	params = map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--param" || arg == "-param":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("--param needs k=v")
			}
			i++
			k, v, ok := strings.Cut(args[i], "=")
			if !ok {
				return "", "", nil, fmt.Errorf("--param %q is not k=v", args[i])
			}
			params[k] = v
		case strings.HasPrefix(arg, "--param="):
			k, v, ok := strings.Cut(strings.TrimPrefix(arg, "--param="), "=")
			if !ok {
				return "", "", nil, fmt.Errorf("--param %q is not k=v", arg)
			}
			params[k] = v
		case strings.HasPrefix(arg, "-"):
			return "", "", nil, fmt.Errorf("unknown flag %q (params go through --param k=v)", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 2 {
		return "", "", nil, fmt.Errorf("invoke needs a unit and a verb, got %d argument(s)", len(positional))
	}
	return positional[0], positional[1], params, nil
}

// printUnits renders the catalog the way a person reads it: one block per
// unit, one line per verb. The columns say where each param travels, because
// that is the difference between a call that works and a 400.
func printUnits(w io.Writer, units []productapi.Unit) {
	if len(units) == 0 {
		fmt.Fprintln(w, "this product declares no invocable units")
		return
	}
	sort.Slice(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	for _, u := range units {
		fmt.Fprintf(w, "%s", u.ID)
		if len(u.Roles) > 0 {
			fmt.Fprintf(w, " (%s)", strings.Join(u.Roles, ", "))
		}
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, v := range u.Verbs {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", v.Verb, v.Method, v.Summary)
			if len(v.Params) > 0 {
				fmt.Fprintf(tw, "\t\t  body: %s\n", strings.Join(v.Params, ", "))
			}
			if len(v.Query) > 0 {
				fmt.Fprintf(tw, "\t\t  query: %s\n", strings.Join(v.Query, ", "))
			}
			if v.Page != "" {
				fmt.Fprintf(tw, "\t\t  page: %s\n", v.Page)
			}
		}
		_ = tw.Flush()
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `product — drive this product from a shell.

  product describe [--json]            what this product offers, and where each param travels
  product invoke <unit> <verb> [--param k=v]...
                                       call one declared verb (same routes the UI uses)

Global: --server <loopback-url> (or GOGOGO_SERVER), --session <token> (or GOGOGO_SESSION)

Discovery first: `+"`describe`"+` is the source of truth for verbs and params. Authorisation
is the product's own session and policy — the same the UI enforces.
`)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
