import io.joern.dataflowengineoss.language.*
import io.shiftleft.semanticcpg.language.*

def runCategory(
  name: String,
  sourceCodeRegex: String,
  sinkNameRegex: String,
  sinkCodeRegex: String,
  flowLimit: Int
): Unit = {
  val sources = cpg.call.code(sourceCodeRegex).l
  val sinks = cpg.call.name(sinkNameRegex).code(sinkCodeRegex).argument.l
  val flows = sinks.reachableByFlows(sources).p

  println(s"=== ${name} ===")
  println(s"sources=${sources.size} sinks=${sinks.size} flows=${flows.size}")
  flows.take(flowLimit).foreach(println)
}

@main def main(
  cpgFile: String,
  sqlSourceRegex: String = ".*req\\.body.*",
  sqlSinkNameRegex: String = "(query|queryPromise|\\$queryRaw.*)",
  sqlSinkCodeRegex: String = ".*",
  cmdSourceRegex: String = ".*req\\.query.*",
  cmdSinkNameRegex: String = "(exec|spawn)",
  cmdSinkCodeRegex: String = ".*",
  xssSourceRegex: String = ".*req\\.params.*",
  xssSinkNameRegex: String = "(send|render)",
  xssSinkCodeRegex: String = ".*",
  pathSourceRegex: String = ".*req\\.body.*",
  pathSinkNameRegex: String = "(readFile|writeFile|readFileSync|writeFileSync|read|write)",
  pathSinkCodeRegex: String = ".*",
  flowLimit: Int = 20
): Unit = {
  importCpg(cpgFile)

  runCategory("sqli", sqlSourceRegex, sqlSinkNameRegex, sqlSinkCodeRegex, flowLimit)
  runCategory("command_injection", cmdSourceRegex, cmdSinkNameRegex, cmdSinkCodeRegex, flowLimit)
  runCategory("xss", xssSourceRegex, xssSinkNameRegex, xssSinkCodeRegex, flowLimit)
  runCategory("path_traversal", pathSourceRegex, pathSinkNameRegex, pathSinkCodeRegex, flowLimit)
}
