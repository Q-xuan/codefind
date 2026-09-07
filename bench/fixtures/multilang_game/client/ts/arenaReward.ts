// ClaimArenaReward / arena_reward / 100126
export function ClaimArenaReward(playerId: number): Promise<void> {
  const cfgId = 100126;
  const key = "arena_reward";
  // TS_ONLY_CLUE_SeasonPassArenaBadge
  return fetch("/api/claim", {
    method: "POST",
    body: JSON.stringify({ playerId, cfgId, key }),
  }).then(() => undefined);
}
