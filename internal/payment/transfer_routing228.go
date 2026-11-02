package payment

import (
	"strconv"
	"strings"
)

// TransferRoute228 deliberately uses tagged values. Earlier settings used 1
// as an enable flag; it must never acquire channel-selection meaning implicitly.
type TransferRoute228 struct {
	Mode      string
	ChannelID int64
}

func ParseTransferRoute228(raw string) (TransferRoute228, error) {
	switch raw {
	case "inherit", "disabled", "manual":
		return TransferRoute228{Mode: raw}, nil
	}
	if strings.HasPrefix(raw, "channel:") {
		value := strings.TrimPrefix(raw, "channel:")
		id, err := strconv.ParseInt(value, 10, 64)
		if err == nil && id > 0 && strconv.FormatInt(id, 10) == value {
			return TransferRoute228{Mode: "channel", ChannelID: id}, nil
		}
	}
	return TransferRoute228{}, ErrInvalid
}

func (r TransferRoute228) String() string {
	if r.Mode == "channel" {
		return "channel:" + strconv.FormatInt(r.ChannelID, 10)
	}
	return r.Mode
}
