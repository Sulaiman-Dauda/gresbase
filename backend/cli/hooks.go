package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gresbase/gresbase"
	"github.com/spf13/cobra"
)

func hooksCommand(gb *gresbase.Gresbase) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage file-based JS hooks (./gb_hooks)",
	}

	var dir string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold the hooks directory with an example hook and type definitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
			created := 0
			for name, content := range map[string]string{
				"types.d.ts":      hooksTypeDefs,
				"example.js.tmpl": hooksExample,
			} {
				path := filepath.Join(dir, name)
				if _, err := os.Stat(path); err == nil {
					continue // never overwrite user files
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					return fmt.Errorf("write %s: %w", path, err)
				}
				created++
			}
			fmt.Printf("Hooks directory ready: %s (%d file(s) created)\n", dir, created)
			fmt.Println("Rename example.js.tmpl to example.js to activate it — *.js files load on boot and hot-reload on change.")
			return nil
		},
	}
	initCmd.Flags().StringVar(&dir, "dir", "./gb_hooks", "Hooks directory to scaffold")

	cmd.AddCommand(initCmd)
	return cmd
}

const hooksExample = `// Gresbase file hook — rename to example.js to activate.
// Files in this directory load at boot and hot-reload on change.
// They run UNSANDBOXED in the server process: trusted code only.
/// <reference path="./types.d.ts" />

registerHook("onRecordCreate", (e) => {
  console.log("record created in", e.collectionName, "id:", e.recordId);

  // Examples:
  // const record = $app.findRecordById(e.collectionName, e.recordId);
  // $app.sendMail("ops@example.com", "New signup", "<b>" + e.recordId + "</b>");
  // $http.post("https://example.com/notify", { id: e.recordId });
});
`

const hooksTypeDefs = `// Type definitions for Gresbase JS hooks (gb_hooks).
// Reference from a hook file with:  /// <reference path="./types.d.ts" />

/** Register a handler for a server event. Hook names include:
 *  onRecordCreate, onRecordUpdate, onRecordDelete,
 *  onCollectionCreate, onCollectionUpdate, onCollectionDelete,
 *  onAuthLogin, onAuthRefresh,
 *  onRecordCreateRequest, onRecordUpdateRequest, onRecordDeleteRequest. */
declare function registerHook(name: string, handler: (event: any) => void): void;

/** Load a registered module by name. */
declare function require(moduleName: string): any;

/** Current time as an RFC3339 string. */
declare function now(): string;

/** Delayed execution (capped at 30s). */
declare function setTimeout(fn: () => void, delayMs: number): void;

declare const console: {
  log(...args: any[]): void;
  info(...args: any[]): void;
  warn(...args: any[]): void;
  error(...args: any[]): void;
  debug(...args: any[]): void;
};

/** Application bridge: database and service access. */
declare const $app: {
  db(): any;
  findRecordById(collection: string, id: string): any;
  findRecordsByFilter(collection: string, filter: string, sort?: string, limit?: number, offset?: number): any[];
  findCollectionByNameOrId(nameOrId: string): any;
  findAdminById(id: string): any;
  sendMail(to: string, subject: string, html: string): void;
  settings(): any;
  logAudit(adminId: string, action: string, resource: string, resourceId: string, data?: any): void;
};

/** Outbound HTTP. */
declare const $http: {
  send(config: { method?: string; url: string; body?: any; headers?: Record<string, string> }): any;
  get(url: string): any;
  post(url: string, body?: any): any;
};

/** OS access (trusted code only). */
declare const $os: {
  getenv(name: string): string;
  readFile(path: string): string;
  writeFile(path: string, content: string): void;
  exec(cmd: string, ...args: string[]): string;
};

/** Crypto/identifier helpers. */
declare const $security: {
  hash(value: string): string;
  randomUUID(): string;
  randomString(length: number): string;
};

/** The event payload of the currently executing hook. */
declare const $event: any;
`
