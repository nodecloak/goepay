package main

func channelAutoStopSetting(raw string) (bool, error) {
	if raw == "" || raw == "false" {
		return false, nil
	}
	if raw == "true" {
		return true, nil
	}
	return false, errConfiguration
}
