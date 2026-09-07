#include "arena_reward.h"
#include <stdio.h>

/* ClaimArenaReward engine binding */
void claim_arena_reward(long player_id) {
    const char *key = "arena_reward";
    int id = ARENA_REWARD_ID; /* 100126 */
    (void)player_id; (void)key; (void)id;
}
