package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/jbadeau/gib"
	"github.com/jbadeau/gib/buildfile"
)

var (
	stepStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	messageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	barFilled    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	barEmpty     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

var phaseLabels = map[gib.ProgressPhase]string{
	gib.PhaseContainerizing: "Containerizing",
	gib.PhasePullingBase:    "Pulling base",
	gib.PhaseBuildingLayer:  "Building layer",
	gib.PhaseBuildingImage:  "Building image",
	gib.PhaseWriting:        "Writing",
	gib.PhaseFinalizing:     "Finalizing",
}

func newProgressWriter(layerCount int) gib.ProgressCallback {
	// Total steps: containerizing, pulling base, N layers, building image, writing, finalizing
	total := 5 + layerCount
	step := 0
	return func(event gib.ProgressEvent) {
		step++
		progress := float64(step) / float64(total)
		if progress > 1 {
			progress = 1
		}

		barWidth := 30
		filled := int(progress * float64(barWidth))
		bar := barFilled.Render(strings.Repeat("█", filled)) +
			barEmpty.Render(strings.Repeat("░", barWidth-filled))

		label := phaseLabels[event.Phase]
		if label == "" {
			label = string(event.Phase)
		}

		// Clear line and overwrite in place
		_, _ = fmt.Fprintf(os.Stderr, "\r\033[K %s %s %s",
			bar,
			stepStyle.Render(label),
			messageStyle.Render(event.Message),
		)

		if event.Phase == gib.PhaseFinalizing {
			_, _ = fmt.Fprintf(os.Stderr, "\r\033[K %s\n", doneStyle.Render("✓ Done"))
		}
	}
}

type buildFlags struct {
	target                  string
	buildFile               string
	context                 string
	parameters              map[string]string
	name                    string
	additionalTags          []string
	from                    string
	credentialHelper        string
	username                string
	password                string
	toCredentialHelper      string
	toUsername              string
	toPassword              string
	fromCredentialHelper    string
	fromUsername            string
	fromPassword            string
	baseImageCache          string
	projectCache            string
	allowInsecureRegistries bool
	sendCredentialsOverHTTP bool
	imageFormat             string
	creationTime            string
	entrypoint              []string
	programArgs             []string
	expose                  []string
	volumes                 []string
	environmentVariables    map[string]string
	labels                  map[string]string
	user                    string
	verbosity               string
	imageMetadataOut        string
}

func newBuildCmd() *cobra.Command {
	f := &buildFlags{
		parameters:           make(map[string]string),
		environmentVariables: make(map[string]string),
		labels:               make(map[string]string),
	}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a container image from a build file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd, f)
		},
	}

	cmd.Flags().StringVarP(&f.target, "target", "t", "", "target image reference or tar://path (required)")
	cmd.Flags().StringVarP(&f.buildFile, "build-file", "b", "jib.yaml", "path to build file")
	cmd.Flags().StringVarP(&f.context, "context", "c", ".", "context root directory")
	cmd.Flags().StringToStringVarP(&f.parameters, "parameter", "p", nil, "template parameters (key=value)")
	cmd.Flags().StringVar(&f.name, "name", "", "image reference for tar targets")
	cmd.Flags().StringSliceVar(&f.additionalTags, "additional-tags", nil, "additional tags")
	cmd.Flags().StringVar(&f.from, "from", "", "base image override")
	cmd.Flags().StringVar(&f.credentialHelper, "credential-helper", "", "credential helper suffix")
	cmd.Flags().StringVar(&f.username, "username", "", "registry username")
	cmd.Flags().StringVar(&f.password, "password", "", "registry password")
	cmd.Flags().StringVar(&f.toCredentialHelper, "to-credential-helper", "", "target credential helper")
	cmd.Flags().StringVar(&f.toUsername, "to-username", "", "target registry username")
	cmd.Flags().StringVar(&f.toPassword, "to-password", "", "target registry password")
	cmd.Flags().StringVar(&f.fromCredentialHelper, "from-credential-helper", "", "base image credential helper")
	cmd.Flags().StringVar(&f.fromUsername, "from-username", "", "base image registry username")
	cmd.Flags().StringVar(&f.fromPassword, "from-password", "", "base image registry password")
	cmd.Flags().StringVar(&f.baseImageCache, "base-image-cache", "", "base image layer cache directory")
	cmd.Flags().StringVar(&f.projectCache, "project-cache", "", "project layer cache directory")
	cmd.Flags().BoolVar(&f.allowInsecureRegistries, "allow-insecure-registries", false, "allow HTTP registries")
	cmd.Flags().BoolVar(&f.sendCredentialsOverHTTP, "send-credentials-over-http", false, "allow sending credentials over HTTP")
	cmd.Flags().StringVar(&f.imageFormat, "image-format", "", "image format (Docker or OCI)")
	cmd.Flags().StringVar(&f.creationTime, "creation-time", "", "creation time (millis or ISO 8601)")
	cmd.Flags().StringSliceVar(&f.entrypoint, "entrypoint", nil, "override entrypoint")
	cmd.Flags().StringSliceVar(&f.programArgs, "program-args", nil, "override cmd")
	cmd.Flags().StringSliceVar(&f.expose, "expose", nil, "override exposed ports")
	cmd.Flags().StringSliceVar(&f.volumes, "volumes", nil, "override volumes")
	cmd.Flags().StringToStringVar(&f.environmentVariables, "environment-variables", nil, "environment variables")
	cmd.Flags().StringToStringVar(&f.labels, "labels", nil, "labels")
	cmd.Flags().StringVar(&f.user, "user", "", "user")
	cmd.Flags().StringVar(&f.verbosity, "verbosity", "lifecycle", "verbosity level")
	cmd.Flags().StringVar(&f.imageMetadataOut, "image-metadata-out", "", "write result JSON to file")

	_ = cmd.MarkFlagRequired("target")

	return cmd
}

func runBuild(cmd *cobra.Command, f *buildFlags) error {
	ctx := cmd.Context()

	// Parse build file
	spec, err := buildfile.Parse(f.buildFile, f.parameters)
	if err != nil {
		return fmt.Errorf("parsing build file: %w", err)
	}

	// Apply CLI overrides to spec
	if f.from != "" {
		if spec.From == nil {
			spec.From = &buildfile.BaseImageSpec{}
		}
		spec.From.Image = f.from
	}
	if f.imageFormat != "" {
		spec.Format = f.imageFormat
	}
	if f.creationTime != "" {
		spec.CreationTime = f.creationTime
	}
	if f.entrypoint != nil {
		spec.Entrypoint = f.entrypoint
	}
	if f.programArgs != nil {
		spec.Cmd = f.programArgs
	}
	if f.expose != nil {
		spec.ExposedPorts = f.expose
	}
	if f.volumes != nil {
		spec.Volumes = f.volumes
	}
	if len(f.environmentVariables) > 0 {
		if spec.Environment == nil {
			spec.Environment = make(map[string]string)
		}
		for k, v := range f.environmentVariables {
			spec.Environment[k] = v
		}
	}
	if len(f.labels) > 0 {
		if spec.Labels == nil {
			spec.Labels = make(map[string]string)
		}
		for k, v := range f.labels {
			spec.Labels[k] = v
		}
	}
	if f.user != "" {
		spec.User = f.user
	}

	// Build convert options with base image credentials
	convertOpts := &buildfile.ConvertOptions{
		AllowInsecureRegistries: f.allowInsecureRegistries,
	}
	fromUsername := f.fromUsername
	fromPassword := f.fromPassword
	if fromUsername == "" {
		fromUsername = f.username
	}
	if fromPassword == "" {
		fromPassword = f.password
	}
	if fromUsername != "" && fromPassword != "" {
		convertOpts.FromUsername = fromUsername
		convertOpts.FromPassword = fromPassword
	}
	fromCredHelper := f.fromCredentialHelper
	if fromCredHelper == "" {
		fromCredHelper = f.credentialHelper
	}
	if fromCredHelper != "" {
		convertOpts.FromCredentialHelper = fromCredHelper
	}

	// Convert spec to builder
	builder, err := buildfile.Convert(spec, f.context, convertOpts)
	if err != nil {
		return fmt.Errorf("converting build file: %w", err)
	}

	// Set up progress display
	if f.verbosity != "error" && f.verbosity != "warn" {
		layerCount := 0
		if spec.Layers != nil {
			layerCount = len(spec.Layers.Entries)
		}
		builder.OnProgress(newProgressWriter(layerCount))
	}

	// Determine target
	var target *gib.Containerizer
	if strings.HasPrefix(f.target, "tar://") {
		tarPath := strings.TrimPrefix(f.target, "tar://")
		var opts []gib.ContainerizerOption
		if f.name != "" {
			opts = append(opts, gib.WithTarImageName(f.name))
		}
		target = gib.ToTar(tarPath, opts...)
	} else {
		var opts []gib.ContainerizerOption
		for _, tag := range f.additionalTags {
			opts = append(opts, gib.WithAdditionalTag(tag))
		}
		if f.allowInsecureRegistries {
			opts = append(opts, gib.WithAllowInsecureRegistries(true))
		}
		if f.sendCredentialsOverHTTP {
			opts = append(opts, gib.WithSendCredentialsOverHTTP(true))
		}

		// Resolve credentials for target
		username := f.toUsername
		password := f.toPassword
		if username == "" {
			username = f.username
		}
		if password == "" {
			password = f.password
		}
		if username != "" && password != "" {
			opts = append(opts, gib.WithCredentials(username, password))
		}

		credHelper := f.toCredentialHelper
		if credHelper == "" {
			credHelper = f.credentialHelper
		}
		if credHelper != "" {
			opts = append(opts, gib.WithCredentialHelper(credHelper))
		}

		target = gib.ToRegistry(f.target, opts...)
	}

	// Build
	result, err := builder.Containerize(ctx, target)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Output result
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	_, _ = fmt.Fprintf(os.Stderr, "\n %s %s\n", labelStyle.Render("Built image:"), valueStyle.Render(result.TargetImage))
	_, _ = fmt.Fprintf(os.Stderr, " %s %s\n", labelStyle.Render("Digest:"), valueStyle.Render(result.Digest.String()))
	_, _ = fmt.Fprintf(os.Stderr, " %s %s\n", labelStyle.Render("Image ID:"), valueStyle.Render(result.ImageID.String()))

	// Write metadata if requested
	if f.imageMetadataOut != "" {
		metadata := map[string]any{
			"image":   result.TargetImage,
			"digest":  result.Digest.String(),
			"imageId": result.ImageID.String(),
			"tags":    result.Tags,
		}
		data, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling metadata: %w", err)
		}
		if err := os.WriteFile(f.imageMetadataOut, data, 0644); err != nil {
			return fmt.Errorf("writing metadata: %w", err)
		}
	}

	return nil
}
