package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-android/sdk"
	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/sliceutil"
	"github.com/kballard/go-shellquote"
)

type config struct {
	AndroidHome      string `env:"ANDROID_HOME"`
	AndroidSDKRoot   string `env:"ANDROID_SDK_ROOT"`
	ID               string `env:"emulator_id,required"`
	StartCommandArgs string `env:"start_command_flags"`
	IsHeadlessMode   bool   `env:"headless_mode,opt[yes,no]"`
}

func main() {
	var cfg config
	if err := stepconf.Parse(&cfg); err != nil {
		failf("Issue with input: %s", err)
	}
	stepconf.Print(cfg)
	fmt.Println()

	if err := ensureGooglePlay(cfg.ID); err != nil {
		failf("Couldn't ensure Play Store is available: %s", err)
	}

	startCustomFlags, err := shellquote.Split(cfg.StartCommandArgs)
	if err != nil {
		failf("Failed to parse start command args, error: %s", err)
	}

	args := []string{
		"@" + cfg.ID,
		"-verbose",
		"-show-kernel",
		"-no-audio",
		"-netdelay", "none",
		"-no-snapshot",
		"-wipe-data",
	}
	if !sliceutil.IsStringInSlice("-gpu", startCustomFlags) {
		args = append(args, []string{"-gpu", "auto"}...)
	}
	if cfg.IsHeadlessMode {
		args = append(args, []string{"-no-window", "-no-boot-anim"}...)
	}
	args = append(args, startCustomFlags...)

	log.Printf("Initialize Android SDK")
	androidSdk, err := sdk.NewDefaultModel(sdk.Environment{
		AndroidHome:    cfg.AndroidHome,
		AndroidSDKRoot: cfg.AndroidSDKRoot,
	})
	if err != nil {
		failf("Failed to initialize Android SDK: %s", err)
	}

	androidHome := androidSdk.GetAndroidHome()
	runningDevices, err := runningDeviceInfos(androidHome)
	if err != nil {
		failf("Failed to check running devices, error: %s", err)
	}

	emulatorPath := filepath.Join(androidHome, "emulator", "emulator")
	serial := startEmulator(emulatorPath, args, androidHome, runningDevices, 1)

	if err := tools.ExportEnvironmentWithEnvman("BITRISE_EMULATOR_SERIAL", serial); err != nil {
		log.Warnf("Failed to export environment (BITRISE_EMULATOR_SERIAL), error: %s", err)
	}
	log.Printf("- Device with serial: %s started", serial)

	log.Donef("- Done")
}
func failf(msg string, args ...interface{}) {
	log.Errorf(msg, args...)
	os.Exit(1)
}
