# Security Policy: leptonai/xray-manager

## Reporting a Vulnerability

Do not open a public GitHub issue or pull request for a suspected vulnerability.

Report vulnerabilities privately through one of these channels:

1. **NVIDIA Vulnerability Disclosure Program (preferred):** [NVIDIA Product Security](https://www.nvidia.com/en-us/security/), using the [vulnerability report form](https://www.nvidia.com/en-us/security/report-vulnerability/).
2. **Email:** [psirt@nvidia.com](mailto:psirt@nvidia.com). Use the [NVIDIA public PGP key](https://www.nvidia.com/en-us/security/pgp-key) when the report contains sensitive evidence.
3. **GitHub private vulnerability reporting:** This repository's **Security** tab, then **Report a vulnerability**.

Include the repository name, the affected commit, the subcommand (`server`, `sync`, or `update`), the vulnerability type, reproduction steps, proof of concept if available, and the impact. Remove object-store keys, SSH keys, proxy user IDs, and host addresses from examples. Use the private channel to share anything that is itself sensitive.

NVIDIA PSIRT acknowledges receipt, validates the report, and coordinates remediation and disclosure. For this repository, remediation is a source change on `main`. There is no packaged release to patch.

## Security Architecture and Context

`xray-manager` is a Go command-line tool for managing a set of Xray proxy servers and the clients that route through them. It is a CLI tool. It opens no listening port and has no build pipeline, no release, and no tags in this repository.

The maintained copy is the `main` branch. The tool has three subcommands:

- `server add|delete|get|ls` (`cmd/server_command.go`): reads an Xray server configuration, converts its inbound (VLESS over WebSocket with TLS, or VLESS over TCP with REALITY) into the matching client outbound (`pkg/xray/config.go`, `pkg/xray/vless.go`), and stores that outbound as a JSON object in a Cloudflare R2 bucket (`pkg/xray/r2.go`, `pkg/s3/`).
- `sync` (`cmd/sync_command.go`): runs beside an Xray client. At an interval of 30 seconds or more it lists the bucket, compares objects by ETag with the JSON files in `--xray-confdir`, adds and removes outbounds on the local Xray instance through its gRPC `HandlerService` at `--xray-server`, and rewrites the files in the configuration directory.
- `update client` (`cmd/update_command.go`, `pkg/xray/installer.go`, `pkg/ssh/ssh.go`): connects to a host with the system `ssh` and `scp`, uploads a bash script, and runs it with `sudo`. The script downloads `xray-linux-<arch>.tar.gz` from the host given in `--domain` and runs the `install.bash` inside it.

Its security responsibility is to move proxy configuration from the operator to every client without letting anyone else change it, and to protect the credentials it is given: the R2 access key, the SSH key, and the proxy user IDs and REALITY parameters inside the stored outbounds.

The boundaries are:

- The R2 API, reached over HTTPS with a static access key and secret passed as flags.
- The local Xray gRPC API, dialed without TLS or authentication (`insecure.NewCredentials()`).
- SSH to target hosts, with host key checking turned off.
- The HTTPS download of the installer package on the target host.
- The local file system: the Xray configuration directory and the temporary files for scripts and keys.

**Repository Exposure Classification:** Public. Basis: `github.com/leptonai/xray-manager` is a public GitHub repository; this document is written to public-safe detail.

**Service Exposure Classification:** External / Regulated (medium confidence). Basis: public source for an operator tool that handles object-store credentials, SSH access, and proxy client credentials; no release or package is published from this repository, and the source does not show where the tool is deployed.

### Threat Model

1. **Write access to the bucket controls every client's routing:** `sync` installs whatever objects the bucket holds. It decodes each object into an Xray outbound and adds it to the running proxy, with no signature and no check beyond Xray's own configuration parser. Anyone who can write to the bucket can send client traffic through a server of their choosing. The code uses the same kind of key for `server` (write) and `sync` (read), so a client host that is given a read-write key carries that authority.
2. **An unverified installer runs as root over SSH:** `update client` runs a script with `sudo` that downloads a package from the host in `--domain` and executes its `install.bash`. There is no checksum or signature check. The integrity of every updated host rests on TLS and on whoever controls that download location.
3. **SSH host keys are not verified:** `pkg/ssh/ssh.go` passes `StrictHostKeyChecking=no` and `UserKnownHostsFile=/dev/null` on every `ssh` and `scp` call. A party on the network path can impersonate the target, receive the uploaded script and its commands, and return its own output.
4. **Credentials appear on command lines and in logs:** The R2 secret is a flag (`--r2-access-key-secret`), so it is visible in the process list and in shell history. `pkg/ssh/ssh.go` logs the full argument list of each `ssh` and `scp` call. The package also supports password authentication, which the CLI flags do not expose today. On that path the logged list includes the password, and the password is also placed in the remote command line for `sudo -S`.
5. **The Xray control API is used without protection:** `sync` dials `--xray-server` in plaintext, and the Xray `HandlerService` it calls has no authentication of its own. If that address is not loopback, anyone who can reach it can add or remove outbounds, and so can any local process on the host.
6. **Synced files and temporary files are exposed to local users:** `WriteFileAtomic` in `pkg/util/fileutil.go` writes the synced outbound files with mode `0644`, and they contain proxy user IDs and REALITY parameters. The temporary names for uploaded scripts and key files come from `math/rand` (`pkg/util/randutil.go`) under the shared temporary directory, and `pkg/ssh/ssh.go` sets a key file to `0644` for a moment before restricting it to `0600`.

### Critical Security Assumptions

- The R2 bucket and its keys are private. Only trusted operators can write to it, and hosts that run `sync` hold keys limited to reading. The tool does not enforce that split.
- The download location given in `--domain` serves only installer packages built by trusted operators. TLS is the only integrity check on that download.
- Target hosts are reached over a network where impersonation is not a concern, because the tool does not verify their identity.
- Trusted operators run the tool on trusted machines. The process list, shell history, logs, and temporary directory of those machines are private.
- The Xray API address given to `sync` is reachable only from the local host.
- The values of `--arch` and `--domain` come from the operator. They are written into the remote script without validation.
- Xray's configuration parser handles the stored JSON safely.
- The check in `sync` that skips an empty bucket listing guards against accidental deletion. It is not a defense against a malicious change.

## Supported Versions

This repository has no tags and no releases. `main` is the only maintained copy. Security reports should name the commit, not a version number.
