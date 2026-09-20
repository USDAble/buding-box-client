// OCTO-FORK: L-D5 signed server dictionary sync — see the product baseline D5.
package productruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

type dictionaryNoticeDTO struct {
	State           string `json:"state"`
	FallbackVersion string `json:"fallbackVersion"`
	RetryAt         string `json:"retryAt,omitempty"`
	Version         string `json:"version,omitempty"`
}

// refreshServerDictionary runs during authenticated workspace initialization.
// Every refusal leaves the accepted cache byte-for-byte intact. The notice
// names both the fallback and recovery event, including manual retries.
func (rt *Runtime) refreshServerDictionary(ctx context.Context) (*dictionaryNoticeDTO, error) {
	if rt.deps.ServerDictionary == nil || rt.deps.Platform == nil {
		return nil, nil
	}

	current, loadErr := rt.deps.ServerDictionary.Load()
	knownVersion := ""
	if loadErr == nil {
		knownVersion = current.Version
	}

	data, err := rt.deps.Platform.SensitiveDictionary(ctx, knownVersion)
	if err != nil {
		return rt.dictionaryFailure(knownVersion), err
	}
	if data.Unchanged {
		if loadErr != nil {
			err = fmt.Errorf("server dictionary: unchanged without a usable cache")
			return rt.dictionaryFailure(""), err
		}
		return rt.dictionarySuccess(current.Version), nil
	}

	trust := rt.deps.CatalogTrust
	dictionary, err := data.SensitiveDictionaryEnvelope.Verify(productclient.VerifyOptions{
		TrustedKeys: trust.TrustedKeys,
		Audience:    trust.Audience,
		Now:         time.Now(),
		Skew:        trust.Skew,
	})
	if err != nil {
		return rt.dictionaryFailure(knownVersion), err
	}

	words, err := applyDictionaryUpdate(dictionary, current, loadErr == nil)
	if err != nil {
		return rt.dictionaryFailure(knownVersion), err
	}
	if !strings.EqualFold(dictionary.SHA256, productclient.SensitiveDictionarySHA256(words)) {
		err = fmt.Errorf("server dictionary: checksum mismatch")
		return rt.dictionaryFailure(knownVersion), err
	}
	if err := rt.deps.ServerDictionary.Put(sensitive.ServerEntry{
		Version:   dictionary.Version,
		KeyID:     dictionary.KeyID,
		Signature: data.DictionarySignature.Sig,
		Words:     words,
		FetchedAt: time.Now().UTC(),
	}); err != nil {
		return rt.dictionaryFailure(knownVersion), err
	}
	return rt.dictionarySuccess(dictionary.Version), nil
}

func applyDictionaryUpdate(update productclient.SensitiveDictionary, current sensitive.ServerEntry, haveCurrent bool) ([]string, error) {
	switch update.Mode {
	case "full":
		if update.Words == nil {
			return nil, fmt.Errorf("server dictionary: full update has no words")
		}
		return append([]string{}, update.Words...), nil
	case "delta":
		if !haveCurrent || update.BaseVersion == "" || update.BaseVersion != current.Version {
			return nil, fmt.Errorf("server dictionary: delta base %q does not match cache", update.BaseVersion)
		}
		removed := make(map[string]struct{}, len(update.Remove))
		for _, word := range update.Remove {
			normalized, ok := sensitive.NormalizeWord(word)
			if !ok {
				return nil, fmt.Errorf("server dictionary: invalid removed word")
			}
			removed[normalized] = struct{}{}
		}
		words := make([]string, 0, len(current.Words)+len(update.Add))
		seen := make(map[string]struct{}, cap(words))
		for _, word := range append(append([]string(nil), current.Words...), update.Add...) {
			normalized, ok := sensitive.NormalizeWord(word)
			if !ok || strings.ContainsAny(word, "\r\n") {
				return nil, fmt.Errorf("server dictionary: invalid word")
			}
			if _, drop := removed[normalized]; drop {
				continue
			}
			if _, duplicate := seen[normalized]; duplicate {
				continue
			}
			seen[normalized] = struct{}{}
			words = append(words, word)
		}
		return words, nil
	default:
		return nil, fmt.Errorf("server dictionary: unsupported mode %q", update.Mode)
	}
}

func (rt *Runtime) dictionaryFailure(version string) *dictionaryNoticeDTO {
	rt.dictionaryMu.Lock()
	rt.dictionaryFailed = true
	rt.dictionaryMu.Unlock()
	return &dictionaryNoticeDTO{State: "degraded", FallbackVersion: version, RetryAt: "next_login"}
}

func (rt *Runtime) dictionarySuccess(version string) *dictionaryNoticeDTO {
	rt.dictionaryMu.Lock()
	recovered := rt.dictionaryFailed
	rt.dictionaryFailed = false
	rt.dictionaryMu.Unlock()
	if !recovered {
		return nil
	}
	return &dictionaryNoticeDTO{State: "recovered", Version: version}
}
