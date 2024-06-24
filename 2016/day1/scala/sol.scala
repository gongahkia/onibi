import scala.collection.mutable

object Taxicab {
  def main(args: Array[String]): Unit = {
    val instructions = "R4, R5, L5, L5, L3, R2, R1, R1, L5, R5, R2, L1, L3, L4, R3, L1, L1, R2, R3, R3, R1, L3, L5, R3, R1, L1, R1, R2, L1, L4, L5, R4, R2, L192, R5, L2, R53, R1, L5, R73, R5, L5, R186, L3, L2, R1, R3, L3, L3, R1, L4, L2, R3, L5, R4, R3, R1, L1, R5, R2, R1, R1, R1, R3, R2, L1, R5, R1, L5, R2, L2, L4, R3, L1, R4, L5, R4, R3, L5, L3, R4, R2, L5, L5, R2, R3, R5, R4, R2, R1, L1, L5, L2, L3, L4, L5, L4, L5, L1, R3, R4, R5, R3, L5, L4, L3, L1, L4, R2, R5, R5, R4, L2, L4, R3, R1, L2, R5, L5, R1, R1, L1, L5, L5, L2, L1, R5, R2, L4, L1, R4, R3, L3, R1, R5, L1, L4, R2, L3, R5, R3, R1, L3"

    // Directions: 0 = North, 1 = East, 2 = South, 3 = West
    var direction = 0
    var x = 0
    var y = 0

    // Set to store visited locations
    val visited = mutable.Set[(Int, Int)]()
    visited.add((x, y))

    val moves = instructions.split(", ")

    // Variable to store the first location visited twice
    var firstRevisitedLocation: Option[(Int, Int)] = None

    for (move <- moves if firstRevisitedLocation.isEmpty) {
      val turn = move.charAt(0)
      val distance = move.substring(1).toInt

      // Update direction
      direction = turn match {
        case 'L' => (direction + 3) % 4 // Turning left is equivalent to turning right 3 times
        case 'R' => (direction + 1) % 4
      }

      // Move in the current direction one block at a time
      for (_ <- 1 to distance if firstRevisitedLocation.isEmpty) {
        direction match {
          case 0 => y += 1 // North
          case 1 => x += 1 // East
          case 2 => y -= 1 // South
          case 3 => x -= 1 // West
        }

        if (visited.contains((x, y))) {
          firstRevisitedLocation = Some((x, y))
        } else {
          visited.add((x, y))
        }
      }
    }

    firstRevisitedLocation match {
      case Some((finalX, finalY)) =>
        val distance = Math.abs(finalX) + Math.abs(finalY)
        println(s"Distance to the first location visited twice: $distance")
      case None =>
        println("No location was visited twice.")
    }
  }
}
