# GoRex

GoRex is a replica of the [Superlogical Rex](https://www.superlogical.com/updates/public-testing-beginning)
terminal. It is written in Go with the native UI of [MyGo](https://mygo.egoist.dev)
and its terminal plugin (libghostty-vt from Ghostty). It uses no webview and
no cgo, and ships as one app of about 15 MB.

![GoRex](docs/screenshot.png)

## What it does

- **Tabs and split panes.** Every pane is a card over the window's gradient.
  The pane header shows the icon, name and directory of the program in the
  pane, and has buttons to split right, split down, zoom and close.
  - Drag the gaps between panes to resize them.
  - Double-click a gap to reset the split to half and half.
- **Persistent sessions.** Shells run in a session server. This is a
  background process of the same binary (`GoRex -server`).
  - When you quit GoRex, everything keeps running. When you open it again,
    every tab, split and pane comes back as it was, and the programs are
    still running.
  - Each session's terminal lives in the server, as a headless
    libghostty-vt emulator that answers its programs. The window shows
    frames of that screen (MyGo's `terminal.Mirror`). Full-screen programs
    and agent interfaces (lazygit, vim, omp…) therefore show exactly what
    the server's terminal has, also after a resize or a reattach.
- **Program activity.** The server watches the foreground process of each
  terminal (`tcgetpgrp`, `sysctl`, `proc_pidinfo`). On Windows it uses the
  newest child of the shell, with its command line and directory.
  - Headers and tabs show the program name (`Node`, `Git Changes` for
    lazygit, `Codex`, `SSH host`…) and its working directory.
  - A green dot shows that a program is printing.
  - An orange dot marks a pane where the program finished, or rang the
    bell, while you were not looking at it.
  - When the window is in the background and a long command finishes,
    GoRex posts a notification.
- **Tab icons.** As in Rex, a tab's icon stacks the tiles of the programs in
  the tab, with the focused pane's tile in front. Drag tabs to reorder them,
  double-click a tab to rename it, and right-click it for a context menu.
- **Host chip.** The chip shows the machine's name and model. Its popover
  shows the chip, memory, OS and session server, and lists the hosts. Each
  host has its machine's icon; the current window's host also has a dot in
  the color of its connection.
  - Click a host to switch the window to it.
  - ⚙ opens the host's settings. The settings of This PC are the Defaults.
  - ⧉ opens the host in a new window.
  - ↻ reconnects.
  - ✕ forgets the host. GoRex asks first.
  - Add Host… adds a new host.
  - Type to filter the list by name or address. Enter switches to the host
    that is left, or to the one you chose with ↑↓. If you type an address,
    only a host with exactly that address matches. If no host matches, Enter
    connects to what you typed, as `ssh user@host` does. The line under the
    field shows which of these Enter will do. Escape clears the field.
  - Right-click the chip for a menu: Reconnect, Host Settings…, Open in New
    Window, Copy Address (`user@host`, the form ssh takes), Copy ssh Command
    (with the host's options), Connection Log…, Send Break and Release Port
    (on a serial port), Restart Session Server…, Switch to the hosts you
    used last, Add Host… and Settings….
  - Restart Session Server… first tells you whether the sessions will keep
    running. They do where the server can replace itself in place, which is
    everywhere except Windows.
- **Command palette** (⇧⌘P, Ctrl+Shift+P, or the ⌘ button). It lists every
  command, every pane you can jump to, and the servers you can switch the
  window to (see below), with fuzzy matching.
- **Import from PuTTY and KiTTY** (Windows only; use Import Hosts from PuTTY /
  KiTTY… in the palette). GoRex turns the saved ssh sessions of PuTTY and
  KiTTY into hosts with the same names.
  - It reads sessions from the registry, or from the folder of a KiTTY that
    stores its sessions in a folder. GoRex finds that folder by its
    kitty.ini, or you choose it.
  - The user, port, agent forwarding, ssh jump host, compression and key
    carry over. A session without a user gets the user from Default
    Settings.
  - Each row shows what does not carry over (other proxies, a remote
    command) and which sessions GoRex already has as hosts.
  - Each row also shows what a session's PuTTY key (.ppk) needs: nothing if
    Pageant already holds the key, otherwise conversion (see below).
- **Pageant and PuTTY keys** (Windows only). In Settings › Hosts, the *ssh
  agent* setting has three choices:
  - *Automatic* (the default): GoRex's ssh uses Pageant, or Kageant from
    KiTTY, when it is running. GoRex then logs you in with the keys loaded
    in it, and Forward ssh agent passes them on. When neither is running,
    GoRex uses the ssh-agent built into Windows.
  - *Pageant*: always use Pageant.
  - *Windows ssh-agent*: always use the Windows ssh-agent.

  The page shows which agent is in use and how many keys it holds. `gorex
  doctor` shows this too.

  ssh cannot read PuTTY's .ppk files. A host may have been imported with a
  .ppk key, or have an identity file that is a .ppk file. For such a host,
  Host Settings shows whether the key is already in the agent. If it is
  not, you can convert the key: enter its passphrase and click Convert. GoRex
  writes the key into your `.ssh` folder in OpenSSH format, with the same
  passphrase and a `.pub` file next to it. It never overwrites a file that
  is already there. Convert PuTTY Key… in the palette converts any key.
  GoRex converts PuTTYgen formats 2 and 3, with RSA, ECDSA and Ed25519 keys.
- **Remote sessions over ssh.** `GoRex ssh user@host` opens a window on the
  sessions of a GoRex server on that host. This works like VS Code's
  Remote-SSH.
  - The first time, GoRex copies the server for the host's system to
    `~/.cache/gorex/<id>/` on the host (or to the host's Server folder) and
    starts it.
  - Every connection goes over one `ssh` connection. `GoRex -bridge` on the
    host multiplexes it with yamux.
  - The remote server keeps its state, including the layout, in
    `~/.config/<profile>`. The profile is the name of the local data
    directory (`GoRex`, `GoRex Dev`).
  - To switch the window to another host or back to this machine, use the
    host chip's popover, or type `ssh user@host` in the palette.
  - When the connection is lost, GoRex reconnects by itself and tries five
    times. A bar under the panes shows the progress, and offers Retry once
    GoRex has given up. The Connection Log (also in the palette) shows what
    ssh reported.
  - Remote hosts must run Linux (glibc 2.17 or later) or macOS, on amd64 or
    arm64.
  - ssh logs in with keys or an agent. Otherwise it asks in the window for
    what it would ask in a terminal: a password, a key's passphrase, a code,
    or whether to trust a host key it has not seen before. It asks again on
    each connection. GoRex does not retry a login that was cancelled or
    refused.
  - `gorex doctor user@host` shows how a host accepts logins.
  - Your ssh agent is forwarded to a host when `~/.ssh/config` has
    `ForwardAgent yes` for it, when the host's Forward ssh agent switch in
    Settings › Hosts is on, or for one window with `GoRex ssh -A user@host`.
    `-a` turns forwarding off and overrides both of the others.
  - Sessions started on the host after that use the agent at a fixed path
    (`~/.config/<profile>/agent.sock`). This path keeps working after a
    reconnect, as long as a window is connected. If several windows are
    connected, the agent of the latest one comes first.
  - A window opened with `GoRex ssh` quits when you close it, on macOS too.
    The server and its sessions keep running.
- **Host settings.** Open them with ⚙ in the host list, with Edit… in
  Settings › Hosts, or with Add Host… (in Settings › Hosts, in the host list,
  or in the palette). They hold:
  - the host's name and its ssh destination;
  - the identity file (a PuTTY key can be converted, see above);
  - more ssh options, such as `-p 2222 -o ProxyJump=bastion`, which every
    ssh connection to the host uses;
  - Forward ssh agent;
  - how GoRex reaches the host:
    - with **GoRex's server** on the host, in the host's Server folder
      (`~/.cache/gorex` when the field is empty). A folder that GoRex
      creates belongs to the user alone, and GoRex never uses another
      user's folder.
    - with **plain ssh**;
    - or as a **serial port** of this PC (see below).
- **Plain ssh hosts.** GoRex reaches a host with ssh alone when its server
  cannot run there, or when you choose this.
  - The host's tabs and panes run `ssh` from this PC. They run in a
    separate session server for that host on this PC (in
    `hosts/<user@host_port>` in the data directory), with their own tabs and
    layout. You switch to the host from the host list like any other host.
  - The sessions outlive the window, but their connections do not. When a
    connection ends in any way other than logging out, the pane says so,
    and Enter connects it again.
  - Features that need GoRex's server on the host are greyed out: editor
    tabs, the host's files, and git.
  - On a plain host, the host's Command runs instead of the login shell.
- **When GoRex's server cannot run on a host.** This happens when there is
  no server for the host's system, when glibc is older than 2.17, or when
  the folder cannot run programs (for example, `/home` mounted noexec). The
  window then follows the setting *When GoRex's server can't run on a host*
  in Settings › General:
  - Ask (the default). The window offers Try Other Folders (when the folder
    could not run programs) and Connect with Plain ssh.
  - Try other folders, then use plain ssh.
  - Use plain ssh.

  The other folders are folders owned by the user in `$XDG_RUNTIME_DIR` (or
  `/run/user/<uid>`), `/var/tmp`, `/dev/shm` and `/tmp`. GoRex uses the
  first one where a test program can run. GoRex saves the result in the
  host's settings (the folder, or plain ssh and the reason), and later
  connections use it.

  `GoRex ssh --plain user@host` connects with plain ssh, and `--server`
  connects with GoRex's server; both override the host's settings. Failed
  logins and unreachable hosts never fall back to plain ssh.
- **Serial ports.** A serial host is a port of this PC, such as the
  console of a switch, a router or a board. To add one, open Add Host…
  and set Connect with to Serial port.
  - Pick the port from a list of what is plugged in. Each entry shows the
    adapter's name above its path, its USB IDs and serial number. The list
    also shows which ports another serial host has open and which are not
    plugged in.
  - Set the speed (115200 by default, or any other value, such as 74880).
  - Set the framing: 8N1, 7E1…, or your own data bits, parity and stop
    bits.
  - Choose what Enter sends (CR, CR LF or LF), what Backspace sends (DEL or
    BS), and local echo.
  - Until you give the host a name, its name is made of the port, the
    adapter and the speed, such as "COM13 · Prolific PL2303 · 115200". The
    host chip also shows the port underneath.
  - The window has one session for the port. It runs in a separate session
    server on this PC (`hosts/serial-…`), which keeps the port open while
    GoRex is closed. There are no more tabs or splits, and no editor tabs.
  - If you unplug the adapter, GoRex waits for it. It finds the adapter
    again by its serial number (or by its USB IDs if it has none), at
    whatever path it comes back on.
  - Send Break (⇧⌘B, Ctrl+Shift+B, the palette, or the Shell menu) sends a
    300 ms break.
  - Release Port closes the port so another program can use it. Press Enter
    in the pane to open it again.
  - If another serial host has the port open, the pane shows which host.
    Press Enter to take the port over; the other host's session then
    releases it, as Release Port does.
  - Settings you change in the host dialog apply to the open port at once.
  - `GoRex serial <name>` opens a window on a serial host.
  - On Linux, a port that belongs to the `dialout` group (`uucp` on some
    systems) needs your user in that group. GoRex tells you this when it
    cannot open the port.
- **Pasting images.** Some programs read the clipboard themselves through
  the Kitty clipboard protocol, as omp does. GoRex tells such a program what
  a paste contains, and the program takes the image. This works on any
  host, so you can paste a screenshot into a prompt over ssh. Other programs
  get the text.
- **Editor tabs** (the ▤ button, ⇧⌘O / Ctrl+Shift+O). An editor tab has a
  browser of the host's files on the left and editors on the right. The
  editors split, zoom, close and take focus like terminal panes.
  - Click a file to open it in the focused editor. If that editor has
    unsaved changes, or if you press Enter or Shift+click, the file opens in
    a new split beside it.
  - The editor is MyGo's editor, with syntax highlighting, multiple carets,
    folding, and find and replace.
  - ⌘S / Ctrl+S saves through the host's server. If something else changed
    the file in the meantime, the server refuses to save and asks what to
    do.
  - A Changes mode shows the diff against the file as it was.
  - Files are read and written on the host (over ssh too) through the
    session server. Buffers live in the window, and unsaved changes survive
    a reconnect.
  - The tab, the browser's root and open folders, and the editor panes and
    their modes come back with the layout.
  - The tree follows the host. The server watches the folders the tree
    shows (with fsnotify), and tells every window when a file there is
    written, by a shell or anything else. An open file that changed on the
    host reloads. If it has unsaved edits, its header shows "changed on
    disk" instead.
  - In a git repository, the tree marks what `git status` reports: **M**
    modified, **A** added, **D** deleted, **R** renamed, **U** untracked,
    **C** conflict. A folder shows the strongest mark inside it. Ignored
    files are dimmed.
- **Settings** (⌘, / Ctrl+,). Settings is a dialog with a sidebar and a
  search over every row.
  - The look of a window follows the host it shows. These settings belong
    to the host, so they apply from any PC:
    - appearance;
    - terminal colors (every Ghostty theme, with a preview);
    - the window background: GoRex's own gradient, neutral grays, or a
      custom color or top-to-bottom gradient that tints it;
    - the font: any monospaced font on this PC. Nerd Font icons show in
      every font, from the embedded Symbols Nerd Font;
    - the cursor.
  - A host without its own value for a setting inherits it from the
    Defaults of this PC.
  - The shell, the folder where new tabs start, the environment and the
    scrollback also belong to the host.
  - These settings belong to this PC: notifications, confirm on close, copy
    on select, reconnecting, what the window title shows (the host, the
    tab, or both), and the window material (Mica, Acrylic or Tabbed on
    Windows 11, or the macOS materials; the gradient becomes translucent
    over it).

## Shortcuts

| macOS | Windows, Linux | |
|---|---|---|
| ⌘T | Ctrl+Shift+T | New tab |
| ⌘D / ⇧⌘D | Ctrl+Shift+D / Ctrl+Shift+E | Split right / down |
| ⌘W / ⇧⌘W | Ctrl+Shift+W | Close pane / tab |
| ⇧⌘↩ | Ctrl+Shift+Enter | Zoom the pane |
| ⌥⌘ arrows | Alt+arrows | Focus the pane in that direction |
| ⌃⌘ arrows | Alt+Shift+arrows | Move the nearest divider |
| ⌃⌘= | | Equalize panes |
| ⌘1…⌘9, ⇧⌘[ ⇧⌘], ⌃Tab | Ctrl+Shift+1…9, Ctrl+PageUp/PageDown, Ctrl+Tab | Switch tabs |
| ⇧⌘R | Ctrl+Shift+R | Rename tab |
| ⇧⌘P, ⌘P | Ctrl+Shift+P | Command palette |
| ⌘K | Ctrl+Shift+K | Clear |
| ⌘+ ⌘− ⌘0 | Ctrl+= Ctrl+- Ctrl+0 | Text size for this host; ⌘0 returns to the Defaults. ⌘ or Ctrl with the mouse wheel also works |
| ⇧⌘O | Ctrl+Shift+O | New editor tab |
| ⌘S, ⇧⌘M | Ctrl+S, Ctrl+Shift+M | Save; show the file's changes (in editor tabs) |
| ⌥⌘Q | | Quit and end all sessions |
| ⇧⌘B | Ctrl+Shift+B | Send Break, on a serial port |

On every system except macOS, Control plus a letter belongs to the terminal
(Ctrl+C, Ctrl+D, Ctrl+W…). GoRex's shortcuts therefore add Shift, as copy
and paste do (Ctrl+Shift+C, Ctrl+Shift+V). No shortcut uses Control and Alt
together, because many keyboards use that combination (AltGr) to type
characters.

Windows-style shortcuts are settings of this PC, and they are on by default
on Windows:

- Ctrl+C copies the selection when there is one, and interrupts when there
  is none.
- Ctrl+V pastes.
- Ctrl+Insert copies and Shift+Insert pastes.
- A right click pastes, or copies the selection if there is one.

## Develop and build

GoRex needs [Go](https://go.dev/dl/) 1.27+ and MyGo 0.6, from the fork that
the `replace` in `go.mod` names. `go.mod` also pins the MyGo CLI as a tool.

```sh
make dev             # this platform's GoRex in dev/, with the servers for ssh hosts
make release         # go tool mygo build: the app and its installer, in build/
go tool mygo dev     # GoRex Dev, rebuilt and restarted as the code changes
go test ./...        # the server, and the view without a window
go tool mygo build   # build/darwin-arm64/GoRex.app and a .dmg
make icon            # redraw resources/icon.png from tools/mkicon/icon.svg
go run ./tools/remotepayload  # the servers for ssh hosts, in resources/remote (mygo build runs it)
```

`go get -tool github.com/egoist/mygo/cmd/mygo@latest` updates MyGo and its
CLI together.

The app keeps its state in its data directory: the server's socket and log,
`layout.json`, the host's `settings.json` and this PC's `defaults.json`. The
built app uses `~/Library/Application Support/GoRex`, and `mygo dev` uses
`GoRex Dev`. This way, development never touches the sessions of the app you
use. Set `GOREX_DIR` to use another directory.

Sessions outlive the app, but not a rebuild. A development build replaces a
server that an older build started. This ends the server's sessions, which
then start again in the same directories. The same applies to servers on
ssh hosts: the first connection after a rebuild copies the new build to the
host and restarts the `GoRex Dev` server there, which ends its sessions.

### Windows

GoRex runs on Windows 10 1809 or later, and uses ConPTY to run the shells.
Its data directory is `%AppData%\GoRex`.

Panes run `$SHELL` when it names a Windows program (`nu`,
`C:\Program Files\Git\bin\bash.exe`). Otherwise they run PowerShell (`pwsh`,
then `powershell`), and if that is missing, `cmd`.

PowerShell's `cd` does not change the directory of the PowerShell process.
GoRex therefore starts PowerShell with a prompt that sets it, wrapped around
the prompt from your profile. The header then follows the directory, and new
panes open there.

The window has no frame. Its title bar draws the minimize, maximize and
close buttons next to the other buttons. You can resize the window from its
sides and bottom, but not from its top.

## Layout

| | |
|---|---|
| `main.go` | the app, its window, `-server` and `-bridge` |
| `bind.go`, `remotes.go` | the server a window shows: connecting, reconnecting, switching, known remotes |
| `plain.go`, `fallback.go`, `hostedit.go` | plain ssh hosts; what a window does when GoRex's server can't run; the host dialog |
| `serialhost.go`, `serialedit.go` | serial hosts: their windows, Send Break and Release Port; the serial port part of the host dialog |
| `state.go` | tabs, the tree of splits, panes, saving and restoring the layout |
| `editor.go`, `browser_view.go`, `editor_view.go` | editor tabs: the file browser, editor panes, opening, saving, what survives a reconnect |
| `view.go`, `tabs.go`, `host.go`, `commands.go` | the interface: title bar, tabs, panes, host popover, menus and palette |
| `programs.go` | how programs are shown: names, glyphs, tile colors |
| `style.go`, `settings.go`, `settings_app.go`, `settings_view.go` | colors, fonts, terminal themes; the layers of settings, the Settings dialog and the theme picker |
| `internal/rex` | the session server and its client: PTYs, foreground processes, headless screens, the socket protocol, the ssh bridge |
| `internal/remote` | reaching a server over ssh: copying the host's server there, the multiplexed link |
| `internal/serialport` | this machine's serial ports as each system lists them; finding an adapter again; opening a port |

`go run ./tools/remotepayload` builds the servers that GoRex copies to ssh
hosts. It writes one directory per platform in `resources/remote/` (Linux
and macOS, amd64 and arm64). Each one is GoRex built for that platform,
with a libghostty-vt beside it:

- On Linux, GoRex's own build, in `remote-libs/`. It is built for glibc
  2.17 by `tools/libbuild`, from the Ghostty commit that the pinned MyGo
  uses.
- On macOS, the library of the MyGo version that `go.mod` pins, from
  MyGo's download cache.

Both are checked against their SHA-256 sums.

When GoRex copies a server to a host, it packs it into a gzipped tar, and
the host's `tar` unpacks it.

`mygo build` runs the tool first, as the `buildCommand` in `mygo.json`. A
build therefore never ships servers older than itself
(`-skip-build-command` skips this step). The build ships the servers next
to the executable, so hosts never download anything. `make dev` builds them
into `dev/remote`. Without them, ssh connections fail with a message that
tells you to run the tool.

Icons: [Lucide](https://lucide.dev) (ISC) and [Simple Icons](https://simpleicons.org)
(CC0). Font: [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (OFL). The ssh
link is multiplexed by [yamux](https://github.com/hashicorp/yamux) (MPL-2.0),
unmodified. Folders are watched with [fsnotify](https://github.com/fsnotify/fsnotify)
(BSD-3).
