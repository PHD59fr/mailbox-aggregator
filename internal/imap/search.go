package imap

import (
	"errors"
	"sort"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const searchWindow = 4096
const maxSearchWindows = 64

var ErrSearchTooBroad = errors.New("search exceeds the safe scan limit")

// Search on the provider, not just the currently downloaded summaries. Bound
// each SEARCH result to a sequence window and fetch at most 50 envelopes.
func filteredPage(client *imapclient.Client, cfg Configuration, page MessagePage, end uint32) (MessagePage, error) {
	expression, err := searchCriteria(cfg)
	if err != nil {
		return page, err
	}
	var matches []protocol.UID
	for window := 0; end > 0 && len(matches) <= PageSize; window++ {
		if window == maxSearchWindows {
			return page, ErrSearchTooBroad
		}
		start := uint32(1)
		if end > searchWindow {
			start = end - searchWindow + 1
		}
		var sequences protocol.SeqSet
		sequences.AddRange(start, end)
		criteria := &protocol.SearchCriteria{SeqNum: []protocol.SeqSet{sequences}, Since: cfg.Since, Before: cfg.Before}
		criteria.And(expression)
		data, err := client.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return page, err
		}
		uids, err := boundedSearchUIDs(data)
		if err != nil {
			return page, err
		}
		for _, uid := range uids {
			matches = append(matches, uid)
			if len(matches) > PageSize {
				break
			}
		}
		end = start - 1
	}
	more := len(matches) > PageSize
	if more {
		matches = matches[:PageSize]
	}
	if len(matches) == 0 {
		return page, nil
	}
	rows, err := client.Fetch(protocol.UIDSetNum(matches...), &protocol.FetchOptions{UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true}).Collect()
	if err != nil {
		return page, err
	}
	for _, row := range rows {
		if row.UID != 0 {
			page.Messages = append(page.Messages, summary(row))
		}
	}
	sort.Slice(page.Messages, func(i, j int) bool { return page.Messages[i].UID > page.Messages[j].UID })
	if more && len(page.Messages) > 0 {
		page.NextBefore = page.Messages[len(page.Messages)-1].UID
	}
	return page, nil
}

func boundedSearchUIDs(data *protocol.SearchData) ([]protocol.UID, error) {
	set, ok := data.All.(protocol.UIDSet)
	if !ok {
		return nil, errors.New("invalid IMAP search result")
	}
	var count uint64
	for _, span := range set {
		if span.Start == 0 || span.Stop < span.Start {
			return nil, errors.New("invalid IMAP search range")
		}
		count += uint64(span.Stop) - uint64(span.Start) + 1
		if count > searchWindow {
			return nil, errors.New("unbounded IMAP search result")
		}
	}
	uids := data.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	return uids, nil
}

func searchCriteria(cfg Configuration) (*protocol.SearchCriteria, error) {
	criteria, err := ParseSearchExpression(cfg.Expression)
	if err != nil {
		return nil, err
	}
	criteria.Since = cfg.Since
	criteria.Before = cfg.Before
	if cfg.Flagged {
		criteria.Flag = append(criteria.Flag, protocol.FlagFlagged)
	}
	if cfg.Search != "" {
		criteria.Text = append(criteria.Text, cfg.Search)
	}
	if cfg.From != "" {
		criteria.Header = append(criteria.Header, protocol.SearchCriteriaHeaderField{Key: "From", Value: cfg.From})
	}
	if cfg.Subject != "" {
		criteria.Header = append(criteria.Header, protocol.SearchCriteriaHeaderField{Key: "Subject", Value: cfg.Subject})
	}
	if cfg.To != "" {
		criteria.Or = append(criteria.Or, [2]protocol.SearchCriteria{
			{Header: []protocol.SearchCriteriaHeaderField{{Key: "To", Value: cfg.To}}},
			{Header: []protocol.SearchCriteriaHeaderField{{Key: "Cc", Value: cfg.To}}},
		})
	}
	if cfg.Seen == "read" {
		criteria.Flag = append(criteria.Flag, protocol.FlagSeen)
	}
	if cfg.Seen == "unread" {
		criteria.NotFlag = append(criteria.NotFlag, protocol.FlagSeen)
	}

	return criteria, nil
}
