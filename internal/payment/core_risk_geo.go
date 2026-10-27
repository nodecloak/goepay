package payment

import (
	"goepay/internal/riskpolicy"
	"strings"
)

func (s *Store) coreRiskGeoAvailable() bool {
	if s.CoreRiskGeo == nil {
		return false
	}
	if ready, ok := s.CoreRiskGeo.(interface{ Ready() bool }); ok {
		return ready.Ready()
	}
	return true
}
func coreRiskNeedsGeo(settings riskpolicy.Settings) bool {
	return settings["pay_region_block"] != "" || settings["pay_block_oversea"] == "1" || settings["check_pay_regoin"] != "0"
}

func (s *Store) checkCoreRiskGeo(settings riskpolicy.Settings, ips []string) error {
	if settings["pay_region_block"] == "" && settings["pay_block_oversea"] != "1" {
		return nil
	}
	if s.CoreRiskGeo == nil || len(ips) == 0 {
		return ErrState
	}
	for _, value := range ips {
		ip, e := riskpolicy.CanonicalIP(value)
		if e != nil {
			return ErrInvalid
		}
		region, found, e := s.CoreRiskGeo.Lookup(ip)
		if e != nil {
			return ErrState
		}
		if !found {
			continue
		}
		if settings["pay_block_oversea"] == "1" && region.Country != "" && region.Country != "中国" && region.Country != "China" {
			return ErrState
		}
		text := region.Country + region.Province + region.City
		for _, blocked := range strings.Split(settings["pay_region_block"], "|") {
			if blocked != "" && strings.Contains(text, blocked) {
				return ErrState
			}
		}
	}
	return nil
}
