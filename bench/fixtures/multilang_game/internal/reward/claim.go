package reward

// ClaimArenaReward grants daily arena chest for config id 100126 (arena_reward).
func ClaimArenaReward(playerID int64) error {
	const rewardKey = "arena_reward"
	const configID = 100126
	return grant(playerID, rewardKey, configID)
}

func grant(playerID int64, key string, id int) error { return nil }
