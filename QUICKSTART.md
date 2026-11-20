# Quickstart

From nothing to running `ls` on your computer from your phone. Plan for 20 to 30 minutes the first time, most of it installing and building.

You will:

1. Put Tailscale on the computer and the phone.
2. Get a TLS certificate for the computer.
3. Run the agent on the computer.
4. Install the app on the phone and pair it.
5. Open a terminal and type `ls`.

If something goes wrong, see [Troubleshooting](#troubleshooting) at the end. Each problem is listed with the exact message you will see.

## What you need

- A computer to control: macOS, Linux or Windows.
- A phone: iPhone or Android 8.0 or newer.
- A free [Tailscale](https://tailscale.com) account.
- To build the agent: [Go](https://go.dev/dl/) (the version in `agent/go.mod`), or a prebuilt archive from the [releases](../../releases) page.
- To build the app, which is not on any store yet:
  - iPhone: a Mac with Xcode, a cable, and an Apple ID (a free one works).
  - Android: Android Studio or just the Android SDK with the Gradle wrapper that comes with the project.

```text
┌──────────────┐   Tailscale (private network)   ┌───────────────────┐
│    Phone     │ ──────────── HTTPS ───────────▶ │ Computer + agent  │
│   the app    │                                 │ real shell (PTY)  │
└──────────────┘                                 └───────────────────┘
```

The agent only listens on the computer's Tailscale address, never on the public internet.

## 1. Install Tailscale on both devices

1. Install Tailscale from [tailscale.com/download](https://tailscale.com/download) on the **computer** and on the **phone**.
2. Sign in on both with the **same account**. Two different accounts are two different networks and will not see each other.
3. On the phone, allow the VPN configuration when it asks. The Tailscale app must show as connected.
4. Check on the computer:

   ```bash
   tailscale status
   ```

   You should see both the computer and the phone in the list.

> **macOS:** if the shell says `tailscale: command not found`, the command line tool is inside the app. Use its full path, or add an alias:
>
> ```bash
> /Applications/Tailscale.app/Contents/MacOS/Tailscale status
> ```

## 2. Turn on MagicDNS and HTTPS certificates

The phone checks that the agent's certificate is valid for the computer's name, so the computer needs a real name and a real certificate. Tailscale gives both for free.

1. Open the [DNS page of the admin console](https://login.tailscale.com/admin/dns).
2. Make sure **MagicDNS** is on.
3. Under **HTTPS Certificates**, choose **Enable HTTPS**.

> **Privacy note:** Tailscale certificates are issued by Let's Encrypt, which records the certificate's name in public certificate transparency logs. That makes your computer's name and your tailnet name (`<computer>.<tailnet>.ts.net`) publicly visible. It does not let anyone connect, but choose a computer name you are happy to have public.

## 3. Get a certificate for the computer

1. Find the computer's full name. It looks like `my-computer.tail1234.ts.net`:

   ```bash
   tailscale status
   ```

   or look at the **Machines** page of the admin console.

2. Create a private folder and ask Tailscale for the certificate there:

   ```bash
   mkdir -p ~/remote-shell-certs && cd ~/remote-shell-certs
   tailscale cert my-computer.tail1234.ts.net
   ```

   This writes two files in the current folder: `my-computer.tail1234.ts.net.crt` (the certificate) and `my-computer.tail1234.ts.net.key` (the private key).

3. Make sure only you can read the key:

   ```bash
   chmod 600 my-computer.tail1234.ts.net.key
   ```

> **Linux:** `tailscale cert` usually needs `sudo`. If you ran it with `sudo`, make the files yours afterwards: `sudo chown $USER: my-computer.tail1234.ts.net.*`, then the `chmod` above.
>
> **Windows:** use PowerShell and your own user folder, for example `C:\Users\you\remote-shell-certs`. The agent must run as a normal user, never as administrator.

Certificates last about 90 days. See [Renewing the certificate](#renewing-the-certificate).

## 4. Get the agent

Pick one.

**Download:** get the archive for your system from the [releases](../../releases) page and unpack `remote-shell` (`remote-shell.exe` on Windows).

**Build:**

```bash
git clone https://github.com/AleixP/remote-shell.git
cd remote-shell
make build            # produces bin/remote-shell
```

Without `make`: `cd agent && go build -o remote-shell ./cmd/remote-shell`.

Check it works:

```bash
./bin/remote-shell version
```

## 5. Check the setup before starting

`doctor` tests everything it can before you start. Use the paths of the two files from step 3:

```bash
./bin/remote-shell doctor \
  -tls-cert ~/remote-shell-certs/my-computer.tail1234.ts.net.crt \
  -tls-key  ~/remote-shell-certs/my-computer.tail1234.ts.net.key
```

You want a `✓` on every line. Before the agent is running, the `Agent` line says `not running`, which is fine for now:

```text
✓ Privileges        running as a normal user
✓ Tailscale         this computer is on the tailnet at 100.x.y.z
✓ TLS certificate   my-computer.tail1234.ts.net is valid for 89 more days (until 2027-02-15)
✓ MagicDNS name     my-computer.tail1234.ts.net resolves to this computer's Tailscale address
✓ File permissions  the key, database and data directory are private to this user
```

A `✗` or `!` tells you what to fix; see [Troubleshooting](#troubleshooting).

## 6. Start the agent

```bash
./bin/remote-shell \
  -tls-cert ~/remote-shell-certs/my-computer.tail1234.ts.net.crt \
  -tls-key  ~/remote-shell-certs/my-computer.tail1234.ts.net.key
```

It prints `agent started` and keeps running. **Leave this terminal open** for now. It listens on `https://<your-tailscale-address>:8443`. Run `doctor` again in another terminal: now every line, including `Agent`, `Exposure` and `TLS handshake`, should be `✓`.

You will make it start by itself in [step 10](#10-keep-it-running).

## 7. Install the app on the phone

The app is not on any store, so you build it from this repository. You need Node.js and npm.

```bash
cd mobile
npm install
npm run build
npx cap sync
```

**iPhone**

1. Open `mobile/ios/App/App.xcodeproj` in Xcode.
2. Select the **App** target, then **Signing & Capabilities**, and choose your **Team** (your Apple ID). The team is not stored in the repository.
3. Connect the iPhone with a cable. On the iPhone, turn on **Developer Mode** (Settings, Privacy & Security, Developer Mode) and restart if it asks.
4. Choose your iPhone as the destination in Xcode and press Run.
5. The first time, the iPhone may say the developer is not trusted: go to Settings, General, VPN & Device Management, and trust it.

> With a **free Apple ID**, iOS stops opening the app after **7 days**. Run it from Xcode again to renew it.

**Android**

```bash
cd mobile/android
./gradlew assembleDebug
```

The app is `app/build/outputs/apk/debug/app-debug.apk`. Install it with `adb install app-debug.apk`, or copy the file to the phone and open it (you will need to allow installing from that source). Android Studio's Run button does the same.

After you change anything in `mobile/`, repeat `npm run build` and `npx cap sync` before building again, or the phone gets the old version.

## 8. Pair the phone

Pairing gives this one phone its own credential. It takes the agent running (step 6).

1. In a **second terminal** on the computer:

   ```bash
   ./bin/remote-shell pair
   ```

   It shows a QR code and says `Valid until HH:MM:SS and usable only once`. The code lasts 5 minutes.

2. On the phone, open the app, tap **+**, then **Pair a computer**, and scan the QR code. Accept the camera permission when it asks.

3. The computer appears in the list with the status **Online**.

**Cannot scan** (no camera permission, or the QR code does not fit your terminal)? Ask for the link as well:

```bash
./bin/remote-shell pair -link
```

It prints the QR code and, below it, a line starting with `remoteshell://pair?`. Send that line to the phone (a note or a message to yourself works), then in the app tap **+**, paste it into **Or paste the pairing link** and tap the button below it. The link carries the same one-time secret as the QR code, so it is only printed when you ask, and anyone who sees it can pair until it expires.

If you started the agent with a custom `-data-dir`, pass the same one to `pair`.

## 9. Your first terminal

1. Tap the computer.
2. Tap **New terminal** and choose **Persistent** (keeps running when you leave the app) or **Temporary** (closes itself after a while with no connection).
3. Type `ls` and press Enter. You are in a real shell on your computer.

Things worth knowing:

- The row of keys above the keyboard has `Esc`, `Tab`, `Ctrl`, the arrows and common symbols, plus **Copy** and **Paste**.
- To select text, **press and hold** on a word, or tap **Select** in the key row and touch or drag over the text. The selection stays, with a **handle** (a blue stem and knob) at each end: drag a handle to adjust that end, and drag it near the top or bottom edge to scroll. A menu floats over the selection with **Copy**, **Select all**, **Paste** and **More** (copy the line, the screen or all the output). Tap anywhere else, or start typing, to dismiss it.
- **Press and hold** a computer or a terminal in the lists to rename it. Names are only stored on your phone.
- Several terminals can be open at once, as tabs.
- Closing a tab does not end the terminal. To end one, use the trash can button, or swipe it in the list.
- Terminals do not survive restarting the agent or the computer.

## 10. Keep it running

So you do not have to start the agent by hand, install it as a background service for your user (no administrator rights needed):

```bash
./bin/remote-shell service install \
  -tls-cert ~/remote-shell-certs/my-computer.tail1234.ts.net.crt \
  -tls-key  ~/remote-shell-certs/my-computer.tail1234.ts.net.key
./bin/remote-shell service status
```

Stop the copy running in your terminal first (Ctrl+C), or they will fight over the port.

The service restarts if it crashes and writes its log to `agent.log` in the data directory. Use `service stop`, `service start` and `service uninstall` to control it.

| System | Starts |
| --- | --- |
| macOS | when you log in |
| Linux | when you log in. To start at boot without logging in: `loginctl enable-linger $USER` |
| Windows | when you log on |

> Install it from a copy of the binary that stays put. The service remembers the path, so do not delete or move the file, and do not point it at `bin/` if you run `make clean`.

If the data directory is in a different place from the default (macOS `~/Library/Application Support/remote-shell`, Linux `~/.config/remote-shell`, Windows `%AppData%\remote-shell`), you have set `-data-dir` yourself.

## Renewing the certificate

Tailscale certificates last about 90 days. Before they run out:

```bash
cd ~/remote-shell-certs
tailscale cert my-computer.tail1234.ts.net
```

It writes new files over the old ones, and the running agent picks them up without a restart. `doctor` warns when fewer than 30 days remain.

## Adding more computers and phones

- **Another computer:** repeat steps 1 to 6 and 8 on it, using that computer's own name and certificate. In the app, tap **+** again.
- **Another phone:** repeat steps 1, 7 and 8 for that phone. Run `remote-shell pair` for each one.
- **Seeing and removing phones:** `remote-shell devices list` and `remote-shell devices revoke <id>`. Revoking cuts that phone off immediately, including open terminals. Removing a computer in the app can revoke that phone for you.

## Troubleshooting

Problems are grouped by where you see them. The text in quotes is what the app or the agent actually shows.

### In the app

**"Cannot reach the computer. Check that Tailscale is connected on this phone and that the agent is running."**
The phone cannot open a connection to the agent. Check, in this order:

1. **Tailscale on the phone** is connected (open its app; the VPN switch must be on).
2. **The computer is awake.** A sleeping computer drops off the tailnet. Check energy settings if it keeps happening.
3. **The agent is running:** `remote-shell service status`, or look at the terminal where you started it. `remote-shell doctor` with the same flags tells you what is wrong.
4. **Both devices are on the same Tailscale account.** In `tailscale status` you should see the phone.
5. **A firewall** is not blocking port 8443. If macOS or Windows asked whether `remote-shell` may accept incoming connections, allow it. Rules for Tailscale's interface must also allow it.
6. **Your Tailscale access rules** (ACLs) allow the phone to reach the computer on that port.

**Computer shows "Offline" in the list**
Same as above. Pull down to refresh after you fix it.

**Computer shows "Access revoked"**
This phone was revoked, or its credential is gone. Remove the computer in the app and pair again.

**"This phone is not authorized on the computer. Remove it and pair again."**
Same fix: remove it and pair again. This also happens if you deleted the agent's data directory.

**"The pairing code is invalid, expired or already used. Run "remote-shell pair" on the computer and scan the new QR code."**
Codes last 5 minutes and work once. Run `remote-shell pair` again and scan the new QR code quickly.

**"Too many attempts. Try again in N seconds."**
The agent locks out a client after too many failed attempts (by default 10 in a minute, for 5 minutes). Wait, then try again with a fresh QR code.

**"Could not scan. Check the camera permission, or paste the pairing link below."**
Give the app the camera permission: on iPhone, Settings, Remote Shell, Camera; on Android, the app's permissions in Settings. Then scan again. Or skip the camera: run `remote-shell pair -link` and paste the link it prints into the app (see [step 8](#8-pair-the-phone)).

**"This is not a Remote Shell pairing code." / "The pairing code is malformed." / "…contains an invalid host."**
You scanned something that is not the agent's QR code, or the QR is cut off, or a pasted link was cut or changed on the way. Make the terminal window larger so the whole QR code is visible and run `remote-shell pair` again; or use `remote-shell pair -link` and paste the whole line.

**"Unsupported pairing code version. Update the app or the agent."**
The app and the agent are from versions that do not understand each other. Update both to the same release.

**"Saving the credential on this phone timed out. Check the connection and try again."**
Despite the ending, this is not a network problem: the phone's secure storage (Keychain or Keystore) did not answer while saving the credential. Close the app completely and pair again. If it keeps happening, restart the phone.

**"The computer reached its maximum number of terminals. End one and try again."**
The agent allows 16 terminals at once by default. End some, or start the agent with a higher `-max-sessions`.

**"That terminal has ended." / "That terminal no longer exists." / "Ended when the agent restarted"**
The shell exited, was closed after being idle, or the agent was restarted. Terminals do not survive that. Open a new one. Ended terminals stay listed for 24 hours; swipe to remove them.

**A banner says "Connection lost. Reconnecting…"**
The network changed or dropped. The app reconnects by itself, usually in a couple of seconds, and catches up with the output it missed. If a message says "some output was lost while disconnected", the connection was down long enough that the agent's buffer (1 MiB per terminal by default) could not keep it all.

**"Not connected. Try again once it reconnects." (when pasting)**
Pasting is refused while there is no connection, so it cannot be lost. Wait for the banner to go away.

**Paste says "Too much text to paste"**
The limit is 1 MiB. Paste in pieces.

**Pasting asks "Paste N lines?"**
The shell you are in does not have "bracketed paste", so each line would run as soon as it arrives. Check the text, then confirm.

**iPhone shows an "Allow Paste" prompt**
iOS asks every time an app reads the clipboard. Choose Allow, or set it to always allow in the app's settings.

**The app stops opening after a week (iPhone)**
Apps installed with a free Apple ID expire after 7 days. Run it from Xcode again.

### Starting the agent

**"no Tailscale address found; is Tailscale running?"**
Tailscale is not running or not signed in on the computer. Start it and check `tailscale status`.

**"-tls-cert and -tls-key are required"**
Give both files, as in step 6.

**"cannot load the certificate and key: …"**
A wrong path, an unreadable file, or a certificate and key that do not belong together. Check the paths. On Linux, check that the key is owned by you after `sudo tailscale cert`.

**Certificate expired: "… expired on …; renew it with `tailscale cert`"**
Run `tailscale cert <name>` again, see [Renewing the certificate](#renewing-the-certificate). The running agent picks it up.

**"running as root/administrator: the agent refuses to start this way"**
By design. Run it as your normal user and do not use `sudo` or an administrator terminal.

**"listen address … is not a Tailscale or loopback address"**
You passed `-listen` with an address that is not the computer's Tailscale address. Leave `-listen` out, and it finds the right one.

**"listen …: address already in use"**
Something is already using port 8443, most likely another copy of the agent (a service, or a terminal you forgot). Stop it, or use `-port` to pick another.

**"another agent is already running (control socket … is in use)"**
Same cause: an agent is already running with this data directory.

**"cannot reach the agent: is it running?" (from `pair` or `devices`)**
Start the agent first. If you started it with `-data-dir`, pass the same `-data-dir` to `pair` and `devices`.

**`pair` shows a QR code that the phone cannot read**
Enlarge the terminal window or lower the font size so the QR fits without wrapping. If it still will not scan, use `remote-shell pair -link` and paste the link into the app.

**"this is a temporary `go run` binary; build the agent first" (from `service install`)**
Build it (`make build`) and run `service install` from that binary, not with `go run`.

### What `doctor` says

| Line | Message | What to do |
| --- | --- | --- |
| Privileges | `running as root/administrator` | Run as your normal user. |
| Tailscale | an error | Start Tailscale and sign in. |
| TLS certificate | `expired on …` or `expires in N days` | Run `tailscale cert <name>` again. |
| MagicDNS name | `does not resolve here …; is MagicDNS enabled?` | Turn on MagicDNS in the admin console, and make sure Tailscale is running on this computer. |
| MagicDNS name | `resolves to …, not to …` | The name points to a different device. Check the name in `tailscale status`. |
| File permissions | `…` (warning) | Make the key private (`chmod 600`) and the data directory yours (`chmod 700`). |
| Agent | `running, but nothing answers on …` | You are using a different port or address than the agent. Pass the same `-port`. |
| Exposure | `the agent answers outside Tailscale on …` | Something answers on the agent's port on one of the computer's other addresses. The agent itself only listens on Tailscale, so this should not happen: stop the agent and report it as a bug, with the full `doctor` output. |
| TLS handshake | `a client validating … would reject the agent` | The certificate does not match the computer's name, or has expired. Request it again. |

### Things that surprise people

**A command in the terminal opens a browser. Where does it open?**
On the **computer**, not on the phone. The shell runs there, and `open`, `xdg-open` or `start` act on the computer's screen. Many tools that log in through a browser also offer a code or a `--no-browser` option; use that and open the URL they print on your phone. Logins that come back to `localhost` only work if you finish them on the computer.

**Closing the app does not stop my terminal.**
That is what **Persistent** means. Temporary terminals close themselves after 30 minutes with no connection by default.

**My terminals disappeared after restarting the computer.**
Terminals live in memory on the computer and do not survive a restart of the agent or the computer. The list shows them as ended.

**I cannot see my computer from outside my home.**
That works by design, over Tailscale, on any network. If it does not, Tailscale is probably not connected on the phone. The agent never needs a port opened on your router.

**Does the agent need to run all the time?**
Only while you want to use it. A service (step 10) is the easiest way to have it always available.

## Still stuck?

Run `remote-shell doctor` with your certificate flags and read every line that is not a `✓`. If that does not explain it, open an issue and include the `doctor` output (the lines hide nothing secret, but you may want to remove your computer's name), the agent version from `remote-shell version`, and the log. If you think you found a security problem, do not post it publicly: see [SECURITY.md](SECURITY.md).
