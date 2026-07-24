package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/core/encoding"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newDecodeJWTCommand() *Command {
	var useStdin bool

	return &Command{
		Name:    "jwt",
		Summary: "Read the header and claims of a JSON Web Token",
		Usage:   "devnest decode jwt <token> [flags]",
		Description: "Decode a JSON Web Token and print what it says about itself: the " +
			"algorithm, the key it names, the claims, and whether it has expired.\n\n" +
			"The signature is never verified, and the output says so in a field rather " +
			"than only here. Verification needs the signing key, a policy for which " +
			"algorithms are acceptable, and a decision about issuer and audience; a " +
			"tool that checks the shape of a signature without any of that teaches " +
			"people to trust a result that means nothing.\n\n" +
			"Nothing is transmitted anywhere. This command exists so that nobody has " +
			"to paste a token into a web page, which hands the token to whoever runs " +
			"the page.\n\n" +
			"A token is a credential. Prefer --stdin: an argument is recorded in your " +
			"shell history and is visible to other processes while the command runs.",
		Examples: []Example{
			{
				Command:     "devnest decode jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhbmEifQ.c2ln",
				Description: "Read a token's header and claims.",
			},
			{
				Command:     "cat token.txt | devnest decode jwt --stdin --output json",
				Description: "Inspect a token without it reaching your shell history.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the token from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			token, err := readSecret(env, args, useStdin, "token")
			if err != nil {
				return err
			}

			result, err := encoding.DecodeJWT(encoding.JWTRequest{Token: token, Now: time.Now()})
			if err != nil {
				return err
			}

			warnAboutToken(env, result)

			return env.Emit(result, jwtText(result))
		},
	}
}

// warnAboutToken raises the two things a person reading a token needs told
// even if they only glance at the output.
//
// Both are warnings rather than errors: the command did what it was asked and
// the token is still worth reading. The exit code stays zero, because "this
// token expired" is a fact about the input, not a failure of the run.
func warnAboutToken(env *Env, result encoding.JWTResult) {
	if result.Claims.Expired {
		env.Warn(errors.CodeCheckFailed, "this token has expired")
	}
	if result.Unsecured {
		env.Warn(errors.CodeCheckFailed,
			"this token declares alg=none, which means it carries no signature at all; "+
				"any correct verifier rejects it")
	}
}

func jwtText(result encoding.JWTResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "algorithm", Value: result.Algorithm},
		}
		if result.Type != "" {
			fields = append(fields, output.Field{Label: "type", Value: result.Type})
		}
		if result.KeyID != "" {
			fields = append(fields, output.Field{Label: "key id", Value: result.KeyID})
		}
		fields = append(fields, output.Field{
			Label: "signature",
			Value: fmt.Sprintf("%d bytes, NOT verified", result.SignatureBytes),
		})
		fields = append(fields, claimFields(result.Claims)...)

		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if err := writeSegment(w, "header", result.Header); err != nil {
			return err
		}
		return writeSegment(w, "payload", result.Payload)
	}
}

// claimFields renders the registered claims that are present. A claim the
// token does not carry is left out rather than shown as empty, so what is on
// screen is what is in the token.
func claimFields(claims encoding.JWTClaims) []output.Field {
	fields := make([]output.Field, 0, 8)

	if claims.Issuer != "" {
		fields = append(fields, output.Field{Label: "issuer", Value: claims.Issuer})
	}
	if claims.Subject != "" {
		fields = append(fields, output.Field{Label: "subject", Value: claims.Subject})
	}
	if len(claims.Audience) > 0 {
		fields = append(fields, output.Field{
			Label: "audience",
			Value: strings.Join(claims.Audience, ", "),
		})
	}
	if claims.ID != "" {
		fields = append(fields, output.Field{Label: "token id", Value: claims.ID})
	}
	if claims.IssuedAt != nil {
		fields = append(fields, output.Field{Label: "issued at", Value: moment(*claims.IssuedAt)})
	}
	if claims.NotBefore != nil {
		value := moment(*claims.NotBefore)
		if claims.NotYetValid {
			value += "  (not valid yet)"
		}
		fields = append(fields, output.Field{Label: "not before", Value: value})
	}

	switch {
	case claims.ExpiresAt == nil:
		fields = append(fields, output.Field{Label: "expires", Value: "never: no exp claim"})
	case claims.Expired:
		fields = append(fields, output.Field{
			Label: "expires",
			Value: moment(*claims.ExpiresAt) + "  (EXPIRED " + since(-claims.SecondsUntilExpiry) + " ago)",
		})
	default:
		fields = append(fields, output.Field{
			Label: "expires",
			Value: moment(*claims.ExpiresAt) + "  (in " + since(claims.SecondsUntilExpiry) + ")",
		})
	}

	return fields
}

// writeSegment prints one decoded segment as indented JSON.
func writeSegment(w io.Writer, name string, segment json.RawMessage) error {
	var indented bytes.Buffer
	if err := json.Indent(&indented, segment, "", "  "); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "cannot print the %s", name)
	}

	if _, err := fmt.Fprintf(w, "\n%s\n%s\n", name, indented.String()); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}
	return nil
}

func moment(at time.Time) string {
	return at.UTC().Format("2006-01-02 15:04:05 UTC")
}

// since renders a span of seconds in the largest unit that keeps it readable.
func since(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}

	switch {
	case seconds < 60:
		return strconv.FormatInt(seconds, 10) + "s"
	case seconds < 3600:
		return strconv.FormatInt(seconds/60, 10) + "m"
	case seconds < 86400:
		return strconv.FormatInt(seconds/3600, 10) + "h"
	default:
		return strconv.FormatInt(seconds/86400, 10) + "d"
	}
}
