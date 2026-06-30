local cjson = require "cjson.safe"
local resty_sha256 = require "resty.sha256"
local str = require "resty.string"

local _M = {
  file = "/etc/lobster-guard/edge-routes.json",
  routes = nil,
  last_error = "not loaded"
}

local function read_file(path)
  local f, err = io.open(path, "rb")
  if not f then
    return nil, err
  end
  local data = f:read("*a")
  f:close()
  return data
end

local function checksum(payload)
  local lines = {
    "version=" .. tostring(payload.version or "") .. "\n",
    "generated_at=" .. tostring(payload.generated_at or "") .. "\n"
  }
  for _, route in ipairs(payload.routes or {}) do
    table.insert(lines, table.concat({
      route.id or "",
      route.project_id or "",
      route.project_name or "",
      route.host or ((route.hosts and route.hosts[1]) or ""),
      route.path_prefix or "",
      route.mode or "",
      route.upstream_url or "",
      route.host_policy or "",
      tostring(route.enabled == true),
      tostring(route.priority or 0),
      route.description or ""
    }, "\t") .. "\n")
  end
  local sha = resty_sha256:new()
  sha:update(table.concat(lines, ""))
  return "sha256:" .. str.to_hex(sha:final())
end

local function validate(payload)
  if type(payload) ~= "table" then
    return nil, "route file is not a JSON object"
  end
  if type(payload.routes) ~= "table" or #payload.routes == 0 then
    return nil, "route file has no valid routes"
  end
  if payload.checksum ~= checksum(payload) then
    return nil, "route file checksum mismatch"
  end
  table.sort(payload.routes, function(a, b)
    local ah = a.host or ((a.hosts and a.hosts[1]) or "")
    local bh = b.host or ((b.hosts and b.hosts[1]) or "")
    if ah ~= bh then return ah < bh end
    if (a.priority or 100) ~= (b.priority or 100) then
      return (a.priority or 100) > (b.priority or 100)
    end
    if #(a.path_prefix or "") ~= #(b.path_prefix or "") then
      return #(a.path_prefix or "") > #(b.path_prefix or "")
    end
    return (a.id or "") < (b.id or "")
  end)
  return payload.routes
end

function _M.configure(path)
  _M.file = path or _M.file
  _M.reload(true)
end

function _M.reload(force)
  local data, err = read_file(_M.file)
  if not data then
    _M.routes = nil
    _M.last_error = err or "route file read failed"
    return nil, _M.last_error
  end
  local payload = cjson.decode(data)
  local routes
  routes, err = validate(payload)
  if not routes then
    _M.routes = nil
    _M.last_error = err
    return nil, err
  end
  _M.routes = routes
  _M.last_error = nil
  return routes
end

function _M.ready()
  local routes, err = _M.reload(false)
  if not routes or #routes == 0 then
    return false, err or _M.last_error or "edge routes not ready"
  end
  return true
end

function _M.match(host, uri)
  local routes, err = _M.reload(false)
  if not routes then
    return nil, err
  end
  host = string.lower(host or "")
  uri = uri or "/"
  for _, route in ipairs(routes) do
    local prefix = route.path_prefix or "/"
    if string.sub(uri, 1, #prefix) == prefix then
      local h = route.host or ((route.hosts and route.hosts[1]) or "")
      if string.lower(h) == host then
        return route
      end
    end
  end
  return nil, "no route matched"
end

function _M.upstream_host(raw)
  local m = ngx.re.match(raw or "", [[^https?://([^/:]+)]], "jo")
  if m then
    return m[1]
  end
  return nil
end

return _M
