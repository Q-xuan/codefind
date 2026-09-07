namespace Game.Client.Arena
{
    // ClaimArenaReward / arena_reward / 100126
    public static class ArenaReward
    {
        public const int ConfigId = 100126;
        public const string Key = "arena_reward";

        public static void ClaimArenaReward(long playerId)
        {
            Net.Send("ClaimArenaReward", ConfigId, Key);
        }
    }
}
