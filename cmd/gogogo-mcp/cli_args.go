// MCP: tool arguments -> the CLI the installer already implements.
//
// Every tool is a thin adapter (main.go's package comment): these functions are
// the whole mapping, so a flag the CLI renames fails here once instead of in
// six literals, and a mutating verb's `confirm` guard lives in one place.
package main

import "errors"

func recipeCLIArgs(subcommand string, args recipeArgs) ([]string, error) {
	if args.Dir == "" {
		return nil, errors.New("dir is required")
	}
	target, err := recipeTargetArgs(subcommand, args)
	if err != nil {
		return nil, err
	}
	cli := append([]string{cliRecipe, subcommand}, target...)
	if err := appendRecipeConfirm(&cli, subcommand, args); err != nil {
		return nil, err
	}
	return cli, nil
}

// recipeTargetArgs names WHAT the verb acts on (a source, an id) and the
// selection flags. Pure: it decides no confirmation.
func recipeTargetArgs(subcommand string, args recipeArgs) ([]string, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	switch subcommand {
	case "install", "update":
		if args.Src == "" {
			return nil, errors.New("src is required (recipe directory, catalog contents directory, or recipe id)")
		}
		out := []string{args.Src, flagDir, args.Dir, flagFormat, format}
		if args.Entry != "" {
			out = append(out, flagEntry, args.Entry)
		}
		return out, nil
	case "status", "outdated":
		out := append([]string{}, optionalID(args.ID)...)
		out = append(out, flagDir, args.Dir, flagFormat, format)
		if args.Catalog != "" {
			out = append(out, flagCatalog, args.Catalog)
		}
		return out, nil
	case "uninstall":
		if args.ID == "" {
			return nil, errors.New("id is required")
		}
		return []string{args.ID, flagDir, args.Dir}, nil
	}
	return nil, nil
}

// appendRecipeConfirm is the guard half: mutating verbs need confirm:true, and
// only install/update can honor a preview.
func appendRecipeConfirm(cli *[]string, subcommand string, args recipeArgs) error {
	if subcommand == "status" || subcommand == "outdated" {
		return nil
	}
	if args.DryRun {
		if subcommand == "uninstall" {
			return errors.New("uninstall has no dry-run; use recipe_status, then confirm:true")
		}
		return nil
	}
	if !args.Confirm {
		return errors.New("refusing without confirm:true (or dryRun:true to preview)")
	}
	*cli = append(*cli, "--yes")
	return nil
}

// optionalID keeps the id slot empty when the caller asked for all recipes.
func optionalID(id string) []string {
	if id == "" {
		return nil
	}
	return []string{id}
}
