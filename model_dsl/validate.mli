val validate :
  source_name:string ->
  source:string ->
  Ast.file ->
  (Ir.t * Diagnostic.t list, Diagnostic.t list) result
