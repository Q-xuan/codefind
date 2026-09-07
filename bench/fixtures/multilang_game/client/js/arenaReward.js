// ClaimArenaReward / arena_reward / 100126
export function ClaimArenaReward(playerId) {
  const cfgId = 100126;
  const key = "arena_reward";
  return fetch("/api/claim", { body: JSON.stringify({ playerId, cfgId, key }) });
}
