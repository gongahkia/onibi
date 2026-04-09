import io.joern.dataflowengineoss.language.*
import io.shiftleft.semanticcpg.language.*

@main def main(
  cpgFile: String,
  sourceCodeRegex: String,
  sinkNameRegex: String,
  sinkCodeRegex: String = ".*"
): Unit = {
  importCpg(cpgFile)

  val sources = cpg.call.code(sourceCodeRegex).l
  val sinks = cpg.call.name(sinkNameRegex).code(sinkCodeRegex).argument.l
  val flows = sinks.reachableByFlows(sources).p

  println(s"sources=${sources.size} sinks=${sinks.size} flows=${flows.size}")
  flows.foreach(println)
}
