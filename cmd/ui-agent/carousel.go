package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nfsarch33/uiauto-framework/pkg/carousel"
)

// carouselCmd renders one design-system slide template to a JPEG via a
// dedicated headless Chrome — the social-content engine's carousel lane.
func carouselCmd() *cobra.Command {
	var templatePath, tokensPath, outPath, kicker, title, body string
	var width, height, quality int

	cmd := &cobra.Command{
		Use:   "carousel",
		Short: "Render a design-system HTML slide template to a fixed-size JPEG in headless Chrome",
		RunE: func(cmd *cobra.Command, args []string) error {
			tmplSrc, err := os.ReadFile(templatePath)
			if err != nil {
				return fmt.Errorf("carousel: read template: %w", err)
			}
			tokensRaw, err := os.ReadFile(tokensPath)
			if err != nil {
				return fmt.Errorf("carousel: read tokens: %w", err)
			}
			var tokens carousel.Tokens
			if err := json.Unmarshal(tokensRaw, &tokens); err != nil {
				return fmt.Errorf("carousel: tokens must be a JSON object: %w", err)
			}

			html, err := carousel.RenderHTML(string(tmplSrc), tokens, carousel.Slide{
				Kicker: kicker, Title: title, Body: body,
			})
			if err != nil {
				return err
			}
			png, err := carousel.Capture(cmd.Context(), html, width, height)
			if err != nil {
				return err
			}
			jpg, err := carousel.ToJPEG(png, quality)
			if err != nil {
				return err
			}
			if err := os.WriteFile(outPath, jpg, 0o644); err != nil {
				return fmt.Errorf("carousel: write %s: %w", outPath, err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "carousel: %s (%dx%d, %d bytes, q%d)\n", outPath, width, height, len(jpg), quality)
			return nil
		},
	}
	cmd.Flags().StringVar(&templatePath, "template", "", "slide template path (Go text/template over .Tokens/.Slide)")
	cmd.Flags().StringVar(&tokensPath, "tokens", "", "design-system tokens.json path")
	cmd.Flags().StringVar(&outPath, "out", "slide.jpg", "output JPEG path")
	cmd.Flags().StringVar(&kicker, "kicker", "", "slide kicker copy")
	cmd.Flags().StringVar(&title, "title", "", "slide title copy")
	cmd.Flags().StringVar(&body, "body", "", "slide body copy")
	cmd.Flags().IntVar(&width, "width", 1080, "slide width in px")
	cmd.Flags().IntVar(&height, "height", 1350, "slide height in px")
	cmd.Flags().IntVar(&quality, "quality", 90, "JPEG quality (1-100)")
	_ = cmd.MarkFlagRequired("template")
	_ = cmd.MarkFlagRequired("tokens")
	return cmd
}
