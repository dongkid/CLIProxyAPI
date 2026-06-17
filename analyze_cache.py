#!/usr/bin/env python3
"""Analyze cache patterns from usage_stats.json and main.log"""
import json, re, os, sys
from datetime import datetime

# 1. Analyze usage_stats.json for cache patterns
stats_path = os.path.join(os.path.dirname(__file__), 'data/usage_stats.json')
with open(stats_path, 'r', encoding='utf-8') as f:
    data = json.load(f)

print("=" * 80)
print("USAGE STATS ANALYSIS")
print("=" * 80)

for api_name, api_data in data.get('apis', {}).items():
    for model_name, model_data in api_data.get('models', {}).items():
        if 'deepseek' in model_name.lower():
            details = sorted(model_data.get('details', []), key=lambda x: x.get('timestamp', ''))
            print(f'\nModel: {model_name}')
            print(f'Total requests: {model_data.get("total_requests")}')

            # Analyze cache patterns
            prev_cached = None
            prev_input = None
            cache_drops = 0
            cache_hits = 0

            for d in details[-50:]:
                tok = d.get('tokens', {})
                cached = tok.get('cached_tokens', 0)
                inp = tok.get('input_tokens', 0)
                out = tok.get('output_tokens', 0)
                lat = d.get('latency_ms', 0)
                failed = d.get('failed', False)
                ts = d.get('timestamp', '')

                if prev_cached is not None and prev_input is not None and not failed:
                    # Detect cache drop: high cache in previous, low cache in current but similar input
                    if cached < prev_cached * 0.3 and prev_cached > 10000 and inp > prev_input * 0.7:
                        cache_drops += 1
                        print(f'  ** CACHE DROP ** {ts} prev_cached={prev_cached:>8} cached={cached:>8} input={inp:>8}/prev_in={prev_input:>8} lat={lat:>6}ms')
                    elif cached > 10000:
                        cache_hits += 1

                prev_cached = cached
                prev_input = inp

            print(f'  Cache hits (high cache): {cache_hits}')
            print(f'  Cache drops: {cache_drops}')

# 2. Analyze main.log for thinking patterns
log_path = os.path.join(os.path.dirname(__file__), 'logs/main.log')
with open(log_path, 'r', encoding='utf-8', errors='replace') as f:
    content = f.read()

print("\n" + "=" * 80)
print("LOG THINKING ANALYSIS")
print("=" * 80)

thinking_entries = []
session_info = {}

for line in content.split('\n'):
    # Track session info
    m = re.search(r'\[([a-f0-9]+)\] .*selector\.go:503.*session=(\S+).*auth=(\S+).*model=(\S+)', line)
    if m:
        req_id = m.group(1)
        auth = m.group(3)
        model = m.group(4)
        session_info[req_id] = {'auth': auth, 'model': model}

    # Track thinking config
    m = re.search(r'\[([a-f0-9]+)\] .*apply\.go:(156|164) .*mode=(\S+) budget=(\S+) level=(\S+)', line)
    if m:
        req_id = m.group(1)
        thinking_entries.append((req_id, 'HAS_THINKING', m.group(3), m.group(5)))

    m = re.search(r'\[([a-f0-9]+)\] .*apply\.go:164.*no config found', line)
    if m:
        thinking_entries.append((m.group(1), 'NO_THINKING', '', ''))

print(f"\nTotal thinking entries in current session: {len(thinking_entries)}")
for req_id, kind, mode, level in thinking_entries[-30:]:
    si = session_info.get(req_id, {})
    print(f"  req={req_id:12s} {kind:15s} mode={mode:10s} level={level:10s} auth={si.get('auth','')[:30]:30s}")

print("\n" + "=" * 80)
print("DEEPSEEK REASONING_EFFORT PATTERN ANALYSIS")
print("=" * 80)

# When does xhigh appear vs no config?
from collections import Counter
effort_pattern = Counter()
for req_id, kind, mode, level in thinking_entries:
    if kind == 'HAS_THINKING':
        effort_pattern[level] += 1
    else:
        effort_pattern['(no thinking)'] += 1

for k, v in effort_pattern.most_common():
    print(f"  {k:20s}: {v}")
