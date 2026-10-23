package imap

import (
	"context"
	"time"

	protocol "github.com/emersion/go-imap/v2"
)

type QuotaResource struct {
	Resource string `json:"resource"`
	Usage    int64  `json:"usage"`
	Limit    int64  `json:"limit"`
	Unit     string `json:"unit"`
}
type Quota struct {
	Supported bool            `json:"supported"`
	Resources []QuotaResource `json:"resources"`
}

func (r Receiver) Quota(ctx context.Context, cfg Configuration) (result Quota, resultErr error) {
	defer r.operation(ctx, cfg, "quota").Result(&resultErr)
	result.Resources = []QuotaResource{}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r.responseBytes = 65536
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return result, err
	}
	defer closeConn()
	if !client.Caps().Has(protocol.CapQuota) {
		return result, nil
	}
	data, err := client.GetQuotaRoot(cfg.mailbox()).Wait()
	if err != nil {
		return result, err
	}
	result.Supported = true
	for _, root := range data {
		for _, kind := range []protocol.QuotaResourceType{protocol.QuotaResourceStorage, protocol.QuotaResourceMessage, protocol.QuotaResourceMailbox, protocol.QuotaResourceAnnotationStorage} {
			resource, ok := root.Resources[kind]
			if !ok {
				continue
			}
			unit := "items"
			if kind == protocol.QuotaResourceStorage || kind == protocol.QuotaResourceAnnotationStorage {
				unit = "KiB"
			}
			result.Resources = append(result.Resources, QuotaResource{Resource: string(kind), Usage: resource.Usage, Limit: resource.Limit, Unit: unit})
			if len(result.Resources) >= 32 {
				return result, nil
			}
		}
	}
	return result, nil
}
