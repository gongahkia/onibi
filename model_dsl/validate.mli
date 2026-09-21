val validate :
  ?commands:Zenbu_model_api.Command_registry.t ->
  source_name:string ->
  source:string ->
  Ast.file ->
  (Ir.t * Diagnostic.t list, Diagnostic.t list) result
