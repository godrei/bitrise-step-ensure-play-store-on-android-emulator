package main

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
)

const (
	bootTimeout         = time.Duration(10) * time.Minute
	deviceCheckInterval = time.Duration(5) * time.Second
	maxBootAttempts     = 5
)

var (
	faultIndicators = []string{" BUG: ", "Kernel panic"}
)

func startEmulator(emulatorPath string, args []string, androidHome string, runningDevices map[string]string, attempt int) string {
	var output bytes.Buffer
	deviceStartCmd := command.New(emulatorPath, args...).SetStdout(&output).SetStderr(&output)

	log.Infof("Starting device")
	log.Donef("$ %s", deviceStartCmd.PrintableCommandArgs())

	// The emulator command won't exit after the boot completes, so we start the command and not wait for its result.
	// Instead, we have a loop with 3 channels:
	// 1. One that waits for the emulator process to exit
	// 2. A boot timeout timer
	// 3. A ticker that periodically checks if the device has become online
	if err := deviceStartCmd.GetCmd().Start(); err != nil {
		failf("Failed to run device start command: %v", err)
	}

	emulatorWaitCh := make(chan error, 1)
	go func() {
		emulatorWaitCh <- deviceStartCmd.GetCmd().Wait()
	}()

	timeoutTimer := time.NewTimer(bootTimeout)

	deviceCheckTicker := time.NewTicker(deviceCheckInterval)

	var serial string
	retry := false
waitLoop:
	for {
		select {
		case err := <-emulatorWaitCh:
			log.Warnf("Emulator process exited early")
			if err != nil {
				log.Errorf("Emulator exit reason: %v", err)
			} else {
				log.Warnf("A possible cause can be the emulator process having received a KILL signal.")
			}
			log.Printf("Emulator log: %s", output)
			failf("Emulator exited early, see logs above.")
		case <-timeoutTimer.C:
			// Include error before and after printing the emulator log because it's so long
			errorMsg := fmt.Sprintf("Failed to boot emulator device within %d seconds.", bootTimeout/time.Second)
			log.Errorf(errorMsg)
			log.Printf("Emulator log: %s", output)
			failf(errorMsg)
		case <-deviceCheckTicker.C:
			var err error
			serial, err = queryNewDeviceSerial(androidHome, runningDevices)
			if err != nil {
				failf("Error: %s", err)
			} else if serial != "" {
				break waitLoop
			}
			if containsAny(output.String(), faultIndicators) {
				log.Warnf("Emulator log contains fault")
				log.Warnf("Emulator log: %s", output)
				if err := deviceStartCmd.GetCmd().Process.Kill(); err != nil {
					failf("Couldn't finish emulator process: %v", err)
				}
				if attempt < maxBootAttempts {
					log.Warnf("Trying to start emulator process again...")
					retry = true
					break waitLoop
				} else {
					failf("Failed to boot device due to faults after %d tries", maxBootAttempts)
				}
			}
		}
	}
	timeoutTimer.Stop()
	deviceCheckTicker.Stop()
	if retry {
		return startEmulator(emulatorPath, args, androidHome, runningDevices, attempt+1)
	}
	return serial
}

func containsAny(output string, any []string) bool {
	for _, fault := range any {
		if strings.Contains(output, fault) {
			return true
		}
	}

	return false
}

func currentlyStartedDeviceSerial(alreadyRunningDeviceInfos, currentlyRunningDeviceInfos map[string]string) string {
	startedSerial := ""

	for serial := range currentlyRunningDeviceInfos {
		_, found := alreadyRunningDeviceInfos[serial]
		if !found {
			startedSerial = serial
			break
		}
	}

	if len(startedSerial) > 0 {
		state := currentlyRunningDeviceInfos[startedSerial]
		if state == "device" {
			return startedSerial
		}
	}

	return ""
}

func queryNewDeviceSerial(androidHome string, runningDevices map[string]string) (string, error) {
	currentRunningDevices, err := runningDeviceInfos(androidHome)
	if err != nil {
		return "", fmt.Errorf("failed to check running devices: %s", err)
	}

	serial := currentlyStartedDeviceSerial(runningDevices, currentRunningDevices)

	return serial, nil
}

func runningDeviceInfos(androidHome string) (map[string]string, error) {
	cmd := command.New(filepath.Join(androidHome, "platform-tools", "adb"), "devices")
	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		log.Printf(err.Error())
		return map[string]string{}, fmt.Errorf("command failed, error: %s", err)
	}

	log.Debugf("$ %s", cmd.PrintableCommandArgs())
	log.Debugf("%s", out)

	// List of devices attached
	// emulator-5554	device
	deviceListItemPattern := `^(?P<emulator>emulator-\d*)[\s+](?P<state>.*)`
	deviceListItemRegexp := regexp.MustCompile(deviceListItemPattern)

	deviceStateMap := map[string]string{}

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		matches := deviceListItemRegexp.FindStringSubmatch(line)
		if len(matches) == 3 {
			serial := matches[1]
			state := matches[2]

			deviceStateMap[serial] = state
		}

	}
	if scanner.Err() != nil {
		return map[string]string{}, fmt.Errorf("scanner failed, error: %s", err)
	}

	return deviceStateMap, nil
}
