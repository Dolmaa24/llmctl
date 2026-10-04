# feat(config): age-encrypted secret store with the passphrase in the OS keyring

## ⚠️ Interface change

This adds one method to `config.SecretStore` in
`llmctl_API_INTERFACE_CONTRACT.md` §9. No existing signature changes. Per
`llmctl_GIT_WORKFLOW.md` §6, every consumer acknowledges before merge.

**Consumers to tag:**

- **Person 3** — `app`: `memSecrets` in `internal/app/demo.go` must gain
  `HasAPIKey`, and `hasSavedKey` in `internal/app/providers.go` can drop its
  `CONTRACT GAP` workaround and call it
- **Person 2** — `provider/*` adapters, which read keys through `GetAPIKey`
- **Person 1** — `doctor/auth_check.go`, which should skip the auth check for
  a provider with no key instead of decrypting to find out

## The gap

`SecretStore` has no way to ask "is a key saved?" without decrypting it. The
provider form needs exactly that to decide whether an empty key field means
"keep the saved key". PR #1 works around it by calling `GetAPIKey` and
discarding the result, which brings a decrypted key into memory for no reason
(SRS NFR-4).

## Proposed addition — §9

```go
type SecretStore interface {
    GetAPIKey(providerID string) (string, error)
    SetAPIKey(providerID, apiKey string) error

    // HasAPIKey reports whether a key is stored for a provider, without decrypting it.
    HasAPIKey(providerID string) (bool, error)
}
```

Also written into §9 as contract notes, because callers already depend on them:

- `GetAPIKey` returns `config.ErrNoSecret` when no key is stored. For Ollama
  that is the normal case.
- `GetProviderConfig` returns `config.ErrNotConfigured` for an unknown provider.

## What this PR implements

- `AgeSecretStore`: all keys in one armored `age` file, `secrets.age`,
  encrypted with a passphrase (SRS NFR-3: age, no custom cryptography). Keys
  are decrypted for the length of one call and never cached or logged.
- `KeyringPassphrase`: the passphrase is a random 256-bit value created on
  first use and kept in the OS keyring (Windows Credential Manager) through
  `go-keyring`, which is pure Go. The user is never prompted.
- `LLMCTL_NO_KEYRING`: when set, a typed passphrase is used instead. This is
  the supported path inside WSL, where no keyring service usually runs. The
  user is asked once per process and the answer is kept in memory only.
- `HasAPIKey` reads `secrets.age.index`, a plain list of provider IDs kept
  beside the encrypted file. It holds no secret. If it is lost or damaged it
  is rebuilt from the encrypted file.
- A keyring that cannot be reached is an error. It never causes a new
  passphrase to be created, which would lock the user out of saved keys.
- A wrong passphrase returns `ErrWrongPassphrase` and cannot overwrite the file.

## One decision worth a second opinion

With a keyring passphrase the file is written at scrypt work factor 10
instead of age's default 18. The default makes every `GetAPIKey` take about a
second, and adapters call it per request. Key stretching only slows down
guessing, and a random 256-bit passphrase cannot be guessed. A typed
passphrase keeps the default.

## Requirements addressed

- SRS **FR-1.2** — keys encrypted at rest, never written in plaintext
- SRS **NFR-3**, **NFR-4**
- Plan **TASK-009**, **SEC-003**

## Checklist (`llmctl_GIT_WORKFLOW.md` §4)

- [x] Targets `main`
- [x] References the requirements it addresses
- [x] Contract document updated in this PR
- [x] `internal/mock` updated
- [ ] Raised in `#interface-changes` and acknowledged before the PR is opened
      (contract §11, Team Setup Guide §5)
- [ ] PR carries the `integration`, `config` and `storage` labels and
      references its issue
- [ ] CI green
- [ ] Acknowledged by Person 3, Person 2, Person 1
