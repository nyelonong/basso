-- Pattern helpers loaded by both the Go engine and the browser player.
-- Written for Lua 5.1 (gopher-lua) and Lua 5.4 (wasmoon) alike.
local insert, sub, find, gmatch = table.insert, string.sub, string.find, string.gmatch
local type, pairs, ipairs, error, select, tostring = type, pairs, ipairs, error, select, tostring

local step_velocity = { x = 1, o = 0.5 }
local step_rest = { ["."] = true, ["-"] = true }
local seq_reserved = { step = true, sample = true, note = true }
local melody_reserved = { step = true, sample = true, note = true, instrument = true, length = true }

local function build(name, fields, options, reserved, velocity)
  local hit = {}
  if options ~= nil then
    if type(options) ~= "table" then error(name .. ": options must be a table", 3) end
    for key, value in pairs(options) do
      if reserved[key] then error(name .. ": options cannot set :" .. tostring(key), 3) end
      hit[key] = value
    end
  end
  for key, value in pairs(fields) do hit[key] = value end
  local scale = hit.velocity
  if scale == nil then scale = 1 end
  if type(scale) ~= "number" then error(name .. ": :velocity must be a number", 3) end
  hit.velocity = velocity * scale
  return hit
end

function seq(sample, pattern, options)
  if type(sample) ~= "string" then error("seq: sample must be a string", 2) end
  if type(pattern) ~= "string" then error("seq: pattern must be a string", 2) end
  local hits, step = {}, 0
  for i = 1, #pattern do
    local char = sub(pattern, i, i)
    if char ~= "|" and not find(char, "^%s$") then
      local velocity = step_velocity[char]
      if velocity ~= nil then
        insert(hits, build("seq", { step = step, sample = sample }, options, seq_reserved, velocity))
      elseif not step_rest[char] then
        error("seq: unknown step " .. char .. " (use x, o, . or -)", 2)
      end
      step = step + 1
    end
  end
  return hits
end

function melody(instrument, pattern, options)
  if type(instrument) ~= "string" then error("melody: instrument must be a string", 2) end
  if type(pattern) ~= "string" then error("melody: pattern must be a string", 2) end
  local hits, step, last = {}, 0, nil
  for token in gmatch(pattern, "[^%s|]+") do
    if token == "_" then
      if last == nil then error("melody: _ must follow a note", 2) end
      last.length = last.length + 1
    elseif token == "." or token == "-" then
      last = nil
    else
      last = build("melody", { step = step, note = token, instrument = instrument, length = 1 },
        options, melody_reserved, 1)
      insert(hits, last)
    end
    step = step + 1
  end
  return hits
end

-- nil arguments are skipped so (stack a (when fill? b)) works.
function stack(...)
  local hits = {}
  for i = 1, select("#", ...) do
    local part = select(i, ...)
    if part ~= nil then
      if type(part) ~= "table" then
        error("stack: argument " .. tostring(i) .. " must be a hit or a list of hits", 2)
      end
      if part.step ~= nil or part.sample ~= nil or part.note ~= nil then
        insert(hits, part)
      else
        for _, hit in ipairs(part) do insert(hits, hit) end
      end
    end
  end
  return hits
end
