import io.joern.dataflowengineoss.language.*
import io.shiftleft.semanticcpg.language.*

@main def main(cpgFile: String, pattern: String = "req", limit: Int = 200): Unit = {
  importCpg(cpgFile)

  val callMatches = cpg.call
    .code(s".*${pattern}.*")
    .l
    .take(limit)
    .map { call =>
      val file = call.file.name.headOption.getOrElse("<unknown>")
      val line = call.lineNumber.map(_.toString).getOrElse("?")
      s"CALL\t${file}:${line}\t${call.name}\t${call.code}"
    }

  val identifierMatches = cpg.identifier
    .code(s".*${pattern}.*")
    .l
    .take(limit)
    .map { ident =>
      val file = ident.file.name.headOption.getOrElse("<unknown>")
      val line = ident.lineNumber.map(_.toString).getOrElse("?")
      s"IDENT\t${file}:${line}\t${ident.name}\t${ident.code}"
    }

  (callMatches ++ identifierMatches).foreach(println)
}
