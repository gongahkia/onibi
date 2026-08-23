
---@meta
--- Zenbu Extension API v1. Generated from zenbu.extension.Contract.
---@class ZenbuExtension
---@field api_version integer
local zenbu = { api_version = 1 }

---@class ZenbuDocument
---@field id string
---@field version integer
---@field length integer

---@class ZenbuSelection
---@field anchor integer
---@field head integer
---@field start integer
---@field stop integer
---@field text string

---@class ZenbuCall
---@field context table
---@field arguments any

---@param start integer
---@param stop integer
---@return string
function zenbu.text(start, stop) end

---@param start? integer
---@param stop? integer
---@return table|nil
function zenbu.syntax(start, stop) end

---@param registration table
function zenbu.command(registration) end
---@param registration table
function zenbu.selector(registration) end
---@param registration table
function zenbu.transform(registration) end
---@class ZenbuBindingRegistration
---@field input string One to sixteen input tokens separated by one ASCII space.
---@field command string
---@field scope? string global, model:<id>, or model:<id>:<status>
---@param registration ZenbuBindingRegistration
function zenbu.bind(registration) end
---@param registration table
function zenbu.on(registration) end

return zenbu
