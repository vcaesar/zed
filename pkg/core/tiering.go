/* Copyright 2022 Zinc Labs Inc. and Contributors
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/zincsearch/zincsearch/pkg/config"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/metadata"
)

// ILM phase names understood by the lifecycle mover.
const (
	ILMPhaseWarm   = "warm"
	ILMPhaseCold   = "cold"
	ILMPhaseDelete = "delete"
)

// StartTiering runs the hot/warm/cold lifecycle mover until ctx is done. It is
// a no-op unless ZINC_TIER_ENABLE is set.
func StartTiering(ctx context.Context) {
	if !config.Global.Tier.Enable {
		return
	}
	interval := config.Global.Tier.CheckInterval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	log.Info().Dur("interval", interval).Msg("tiering: lifecycle mover started")
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("tiering: lifecycle mover stopped")
				return
			case now := <-tick.C:
				runTieringOnce(now)
			}
		}
	}()
}

// runTieringOnce applies the lifecycle of every index once, using now as the
// current time. Errors are logged and never stop the other indexes.
func runTieringOnce(now time.Time) {
	for _, index := range ZINC_INDEX_LIST.List() {
		lifecycle, err := ResolveLifecycle(index)
		if err != nil {
			log.Error().Err(err).Str("index", index.GetName()).Msg("tiering: lifecycle")
			continue
		}
		if lifecycle == nil {
			continue
		}
		age, ok := indexAge(index, now)
		if !ok {
			continue
		}
		name := index.GetName()
		action, tier := nextTierAction(lifecycle, index.GetTier(), age)
		switch action {
		case ILMPhaseDelete:
			log.Info().Str("index", name).Dur("age", age).Msg("tiering: deleting expired index")
			if err := DeleteIndex(name); err != nil {
				log.Error().Err(err).Str("index", name).Msg("tiering: delete index")
			}
		case ILMPhaseWarm, ILMPhaseCold:
			log.Info().Str("index", name).Str("tier", tier).Dur("age", age).Msg("tiering: moving index")
			if err := MoveIndexTier(name, tier); err != nil {
				log.Error().Err(err).Str("index", name).Str("tier", tier).Msg("tiering: move index")
			}
		}
	}
}

// nextTierAction returns the phase an index of the given tier and age must
// enter, and the tier that phase moves it to. An empty action means no change.
func nextTierAction(lifecycle *meta.IndexLifecycle, tier string, age time.Duration) (action, next string) {
	if d, ok := phaseAge(lifecycle.DeleteAfter); ok && age >= d {
		return ILMPhaseDelete, ""
	}
	if d, ok := phaseAge(lifecycle.ColdAfter); ok && age >= d && tier != TierCold {
		return ILMPhaseCold, TierCold
	}
	if d, ok := phaseAge(lifecycle.WarmAfter); ok && age >= d && tier == TierHot {
		return ILMPhaseWarm, TierWarm
	}
	return "", ""
}

// ExplainTiering reports how old an index is and which lifecycle phase it is
// heading for: the phase due now, or the next one it will reach.
func ExplainTiering(index *Index, lifecycle *meta.IndexLifecycle, now time.Time) (age time.Duration, action, next string) {
	age, ok := indexAge(index, now)
	if !ok || lifecycle == nil {
		return age, "", ""
	}
	tier := index.GetTier()
	if action, next = nextTierAction(lifecycle, tier, age); action != "" {
		return age, action, next
	}
	for _, phase := range []struct{ name, minAge, tier string }{
		{ILMPhaseWarm, lifecycle.WarmAfter, TierWarm},
		{ILMPhaseCold, lifecycle.ColdAfter, TierCold},
		{ILMPhaseDelete, lifecycle.DeleteAfter, ""},
	} {
		if _, ok := phaseAge(phase.minAge); !ok {
			continue
		}
		if phase.name == ILMPhaseWarm && tier != TierHot {
			continue
		}
		if phase.name == ILMPhaseCold && tier == TierCold {
			continue
		}
		return age, phase.name, phase.tier
	}
	return age, "", ""
}

// indexAge is how long ago the newest document of the index was written. The
// document time is tracked per shard, the index level copy of the stats is not
// maintained, so every shard is consulted.
func indexAge(index *Index, now time.Time) (time.Duration, bool) {
	index.lock.RLock()
	docTimeMax := index.ref.Stats.DocTimeMax
	for _, shard := range index.ref.Shards {
		if shard.Stats.DocTimeMax > docTimeMax {
			docTimeMax = shard.Stats.DocTimeMax
		}
		for _, second := range shard.Shards {
			if second.Stats.DocTimeMax > docTimeMax {
				docTimeMax = second.Stats.DocTimeMax
			}
		}
	}
	index.lock.RUnlock()
	if docTimeMax <= 0 {
		return 0, false
	}
	return now.Sub(time.Unix(0, docTimeMax)), true
}

// ResolveLifecycle returns the lifecycle of an index with the phases of its ILM
// policy filled in. Values set directly on the index win over the policy.
func ResolveLifecycle(index *Index) (*meta.IndexLifecycle, error) {
	// the lifecycle is copied under the index lock, settings are mutated in place
	index.lock.RLock()
	if index.ref.Settings == nil || index.ref.Settings.Lifecycle == nil {
		index.lock.RUnlock()
		return nil, nil
	}
	lifecycle := *index.ref.Settings.Lifecycle
	index.lock.RUnlock()
	if lifecycle.Policy == "" {
		return &lifecycle, nil
	}
	record, err := metadata.ILM.Get(lifecycle.Policy)
	if err != nil {
		return nil, fmt.Errorf("ilm policy [%s]: %w", lifecycle.Policy, err)
	}
	if lifecycle.WarmAfter == "" {
		lifecycle.WarmAfter = record.Policy.Phases[ILMPhaseWarm].MinAge
	}
	if lifecycle.ColdAfter == "" {
		lifecycle.ColdAfter = record.Policy.Phases[ILMPhaseCold].MinAge
	}
	if lifecycle.DeleteAfter == "" {
		lifecycle.DeleteAfter = record.Policy.Phases[ILMPhaseDelete].MinAge
	}
	return &lifecycle, nil
}

// phaseAge parses an ILM min_age such as "7d", "12h" or "30m". An empty or
// unparsable value disables the phase.
func phaseAge(value string) (time.Duration, bool) {
	d, err := ParseAge(value)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// ParseAge parses an ILM duration, adding the "d" (days) unit that
// time.ParseDuration does not support.
func ParseAge(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("age is empty")
	}
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid age [%s]: %w", value, err)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid age [%s]: %w", value, err)
	}
	return d, nil
}
