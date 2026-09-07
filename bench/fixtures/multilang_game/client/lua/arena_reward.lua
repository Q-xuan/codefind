-- arena_reward client handler; config id 100126
local ArenaReward = {}

function ArenaReward.ClaimArenaReward(player)
  local cfg_id = 100126
  local key = "arena_reward"
  -- LUA_ONLY_CLUE_MoonlitArenaChest
  return net.send("ClaimArenaReward", cfg_id, key)
end

return ArenaReward
