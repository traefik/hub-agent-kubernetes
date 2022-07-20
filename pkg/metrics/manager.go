/*
Copyright (C) 2022 Traefik Labs

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package metrics

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
)

const scrapeInterval = time.Minute

// Manager orchestrates metrics scraping and sending.
type Manager struct {
	store      *Store
	client     *Client
	traefikURL string
	scraper    *Scraper

	sendMu     sync.Mutex
	sendIntvl  time.Duration
	sendTables []string

	state atomic.Value
}

// NewManager returns a manager.
func NewManager(client *Client, traefikURL string, store *Store, scraper *Scraper) *Manager {
	var st atomic.Value
	st.Store(&state.Cluster{})

	return &Manager{
		store:      store,
		client:     client,
		traefikURL: traefikURL,
		scraper:    scraper,
		sendIntvl:  time.Minute,
		sendTables: []string{"1m", "10m", "1h", "1d"},
		state:      st,
	}
}

// SetConfig updates the configuration of the metrics manager.
func (m *Manager) SetConfig(sendInterval time.Duration, sendTables []string) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	m.sendIntvl = sendInterval
	m.sendTables = sendTables
}

// TopologyStateChanged is called every time the topology state changes.
func (m *Manager) TopologyStateChanged(_ context.Context, cluster *state.Cluster) {
	if cluster == nil {
		return
	}

	m.state.Store(cluster)
}

// Run runs the metrics manager. This is a blocking method.
func (m *Manager) Run(ctx context.Context) error {
	prevData, err := m.client.GetPreviousData(ctx, true)
	if err != nil {
		return err
	}

	for tbl, data := range prevData {
		if err = m.store.Populate(tbl, data); err != nil {
			return fmt.Errorf("unable to populate table: %w", err)
		}
	}

	go m.startScraper(ctx)
	go m.runSender(ctx)

	<-ctx.Done()

	return nil
}

func (m *Manager) runSender(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case <-time.After(m.getSendInterval()):
			if err := m.send(ctx, m.getSendTables()); err != nil {
				log.Error().Err(err).Msg("Unable to send metrics")
			}
		}
	}
}

func (m *Manager) getSendInterval() time.Duration {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	return m.sendIntvl
}

func (m *Manager) getSendTables() []string {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	return m.sendTables
}

func (m *Manager) send(ctx context.Context, tbls []string) error {
	m.store.RollUp()

	toSend := make(map[string][]DataPointGroup)
	tblMarks := make(map[string]WaterMarks)
	for _, name := range tbls {
		tbl := name

		tblMarks[tbl] = m.store.ForEachUnmarked(tbl, func(edgeIngr, ingr, svc string, pnts DataPoints) {
			toSend[tbl] = append(toSend[tbl], DataPointGroup{
				EdgeIngress: edgeIngr,
				Ingress:     ingr,
				Service:     svc,
				DataPoints:  pnts,
			})
		})
	}

	if len(toSend) == 0 {
		return nil
	}

	if err := m.client.Send(ctx, toSend); err != nil {
		return err
	}

	for tbl, marks := range tblMarks {
		m.store.CommitMarks(tbl, marks)
	}
	m.store.Cleanup()

	return nil
}

func (m *Manager) startScraper(ctx context.Context) {
	mtrcs, err := m.scraper.Scrape(ctx, ParserTraefik, m.traefikURL, ScrapeState{
		Ingresses: m.getIngresses(),
	})
	if err != nil {
		log.Error().Err(err).Msg("Unable to scrape metrics")
		return
	}

	ref := Aggregate(mtrcs)

	tick := time.NewTicker(scrapeInterval)
	defer tick.Stop()

	scrapeSec := int64(scrapeInterval.Seconds())
	for {
		select {
		case <-ctx.Done():
			return

		case <-tick.C:
			mtrcs, err = m.scraper.Scrape(ctx, ParserTraefik, m.traefikURL, ScrapeState{
				Ingresses: m.getIngresses(),
			})
			if err != nil {
				log.Error().Err(err).Msg("Unable to scrape metrics")
				return
			}

			mtrcSet := Aggregate(mtrcs)

			ts := time.Now().UTC().Truncate(time.Minute).Unix()

			pnts := make(map[SetKey]DataPoint, len(mtrcSet))
			for key, mtrc := range mtrcSet {
				mtrc = mtrc.RelativeTo(ref[key])

				pnt := mtrc.ToDataPoint(scrapeSec)
				pnt.Timestamp = ts
				pnt.Seconds = scrapeSec

				pnts[key] = pnt
			}

			m.store.Insert(pnts)

			ref = mtrcSet
		}
	}
}

func (m *Manager) getIngresses() map[string]struct{} {
	cluster := m.state.Load().(*state.Cluster)

	ingresses := make(map[string]struct{}, len(cluster.Ingresses))
	for name := range cluster.Ingresses {
		ingresses[name] = struct{}{}
	}

	return ingresses
}
