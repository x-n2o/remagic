# Paper Pro beta 3.29 validation

Validated on 2026-10-18, reMarkable Paper Pro (Ferrari), OS **3.29.0.149**,
Qt **6.10.3**. The firmware was kept unchanged.

## Components

| Component | Source |
| --- | --- |
| xovi + Qt resource rebuilder | `asivery/rm-xovi-extensions` `v19-23052026` |
| AppLoad | `asivery/rm-appload` `v0.6.0`, `appload-aarch64.tar.gz` |
| Quill | PR [#5](https://github.com/MaximeRivest/quill/pull/5), head `6d0fded7703c58bc52f77539ed62583e2b4d6a85`, plus build/test changes |
| Riddle | upstream `0d1a9fe`, plus local build, recovery and request fixes |
| OpenRouter | `https://openrouter.ai/api/v1`, `openai/gpt-4.1-mini` |

The served Riddle 0.3.0 release archive has SHA-256
`1f37eac57183b664148b226fe285d04f75d9d43d4e5edfc532be9ae6b57e5461`,
also reported by GitHub's release asset API. The catalog's previous hash was
stale. This checksum correction does **not** fix that release's Quill ABI;
the beta tablet uses a locally built bundle instead.

## Results

- `go test ./...`, `go vet ./...`, and Bash syntax checks passed. Focused tests
  cover firmware/archive selection, key skipping, password reconnects, bounded
  SSH handshakes, hook staging/quarantine and resource/startup failure recovery.
- Setup rebuilt a complete 668947-byte resource table. AppLoad reported
  `Loaded external AppLoad hooks in main UI`; xochitl mapped the new extension
  and remained stable without the old QML hash panic.
- After a controlled reboot, the existing physical triple-press shortcut
  restarted xochitl and loaded AppLoad successfully. The boot ID stayed the
  same during that shortcut. My Files remains the home screen; AppLoad is an
  entry in its top-left menu, rather than an automatically opened screen.
- Quill's on-device acceptance suite returned zero failures, including
  initialization, 1620×2160 geometry, clipping, invalid rectangles, partial
  update stress and dynamic-linker interposition probes.
- A synthetic `2+2=?` image sent through Riddle's on-device oracle test received
  a correct response through OpenRouter. No notebook was used for this test.
- Riddle normal shutdown and an actual child-process SIGKILL restored xochitl,
  released the diary wakelock and preserved the boot ID. An administrative
  SIGKILL of the entire service control group also killed cleanup; that test
  required manual UI recovery and is not counted as passing.
- New SSH connections through the dedicated 1Password agent key succeeded.
  Hashes of all pre-existing local SSH key files and SSH configuration were
  unchanged; the tablet's original authorized-key entries were preserved.
- Existing healthy tripletap was reused with `TRIGGER_ACTION="start"` and
  version switching disabled. Root was confirmed read-only after deployment
  and reboot. The API configuration is mode 0600.

The user subsequently confirmed the AppLoad/Diary flow and handwritten exchange
work. The original sleep/wake test subsequently froze on the diary's sleep page
with USB unavailable, requiring a physical restart. Riddle's replacement
sleep flow waits for panel discharge without blocking input, then waits for
the actual suspend/resume transaction rather than its enqueue acknowledgement.
An injected power pair restored the page immediately; an injected five-finger
gesture exited cleanly. The corrected deep-sleep test incremented the kernel
success counter and woke via a temporary RTC alarm, without rebooting.
Physical power-button wake and unplugged launcher visibility remain pending
user confirmation. Riddle's cinematic pacing option is selected and active;
its visual timing also awaits user acceptance.

## Recovery

The original installation, service configuration and authorized keys were
backed up under `/home/root/remagic-backup-20261018/` before deployment.
No notebooks were changed. The locally installed application configuration
is retained when refreshing its bundle.

Repeated deliberate UI takeovers can exhaust the firmware's xochitl restart
limit. Recovery therefore resets that counter before starting stock xochitl.
Setup also validates new hooks with automatic restart/reboot actions
temporarily suppressed and quarantines AppLoad if validation fails.
