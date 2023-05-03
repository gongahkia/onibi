// to resolve && add
// - if needed, do the following in a separate isolated rust file
    // - add ability to edit tasks and save new tasks
    // - modularize the functions used in this program to prevent it from becoming one massive file
    // - ability to edit items

// ----------

use std::io;
use std::fmt;
use std::fs;
use std::str::FromStr;
use std::fs::File;
use std::io::Write;

#[derive(Debug)]
struct Task {
    task_name:String, // eg. do math homework
    task_description:String, // eg. submit them via managebac after completing them via linkedin
    task_deadline:[i32; 3],
    task_urgency:UrgencyLevel,
}

#[derive(Debug, Clone, Copy)]
enum UrgencyLevel {
    Low,
    Medium,
    High,
}

// allow for displaying of enum as a string 
impl fmt::Display for UrgencyLevel {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            UrgencyLevel::Low => write!(f, "Low"),
            UrgencyLevel::Medium => write!(f, "Medium"),
            UrgencyLevel::High => write!(f, "High"),
        }
    }
}

// allow for conversion of string to enum 
impl FromStr for UrgencyLevel {
    type Err = ();
    fn from_str(s: &str) -> Result<UrgencyLevel, ()> {
        match s {
            "Low" => Ok(UrgencyLevel::Low),
            "Medium" => Ok(UrgencyLevel::Medium),
            "High" => Ok(UrgencyLevel::High),
            _ => Err(()),
        }
    }
}

fn main() {

    let mut storage_vector:Vec<Task> = vec![];

    // reading of local file and parsing it into the struct Task
    let file_contents_results = fs::read_to_string(".kelpStorage");
    // handling of error values
    let file_contents = match file_contents_results {
        Ok(string) => {
        let file_contents_array = string.trim_end().split("\n");
        let file_contents_vector:Vec<&str> = file_contents_array.collect();
        //println!("{:?}", file_contents_vector);
        for eachtask in &file_contents_vector {
            let each_task_array:Vec<&str> = eachtask.split(", ").collect();
            let each_task_deadline:[i32;3] = [each_task_array[2][..2].trim_end().parse().unwrap(), each_task_array[2][2..4].trim_end().parse().unwrap(), each_task_array[2][4..].trim_end().parse().unwrap()];
            println!("{:?}", each_task_deadline);
            match each_task_array[3].parse::<UrgencyLevel>() {
                //each_task_array[3].parse::<UrgencyLevel>()
                Ok(level) => {
                    let each_task_urgency:UrgencyLevel = level;
                    let the_given_task = Task {
                        task_name: String::from(each_task_array[0]),
                        task_description: String::from(each_task_array[1]),
                        task_deadline: each_task_deadline,
                        task_urgency: each_task_urgency,
                        };
                    storage_vector.push(the_given_task);
                    println!("{:?}", storage_vector);
                },
                Err(_) => (),
            }
        }
        },
        Err(_) => println!("Failed to read file. Loading new save."),
    };

    loop {
        
        // break condition
        println!("[E]xit / [Enter] to add task: ");
        let mut exit_condition:String = String::new();
        io::stdin().read_line(&mut exit_condition).expect("Failed to read line");
        let exit_condition_str:&str = exit_condition.as_str().trim_end();
        if exit_condition_str == "e" {
            // writing of all tasks to a local file titled .kelpStorage
            let mut save_file = File::create(".kelpStorage").expect("File already exists");
            for eachtask in &storage_vector {
                let task_deadline_string:String = eachtask.task_deadline.into_iter().map(|i| i.to_string()).collect::<String>();
                write!(save_file, "{}, {}, {}, {}\n", eachtask.task_name, eachtask.task_description, task_deadline_string, eachtask.task_urgency.to_string());
            }
            break;
        }

        // task name
        println!("Enter task name: ");
        let mut userinput_task_name:String = String::new();
        io::stdin().read_line(&mut userinput_task_name).expect("Failed to read line");
        let userinput_task_name = String::from(userinput_task_name.trim_end());
        
        // task description
        println!("Enter task description: ");
        let mut userinput_task_description:String = String::new();
        io::stdin().read_line(&mut userinput_task_description).expect("Failed to read line");
        let userinput_task_description = String::from(userinput_task_description.trim_end());

        // task deadline -> parsed using destructuring
            // for future reference:
            // error was initially occuring due to newline character of last element in vector, need
            // to remember to use .trim_end() method to remove said newline character
        println!("Enter task deadline in the following format [DD/MM/YY]: ");
        let userinput_task_deadline_formatted:[i32; 3];

        loop {
            let mut userinput_task_deadline_raw:String = String::new();
            io::stdin().read_line(&mut userinput_task_deadline_raw).expect("Failed to read line");
            let userinput_task_deadline_raw_array = userinput_task_deadline_raw.split("/");
            let userinput_task_deadline_array: Vec<&str> = userinput_task_deadline_raw_array.collect();
            
            // checking for valid number of fields input (characters, str literals and numbers covered)
            if userinput_task_deadline_array.len() != 3 {
                println!("Invalid input detected.\nEnter task deadline in the following format [DD/MM/YY]: ");
                continue;
            }

            // checking for characters instead of date input if there are 3 fields
                // for future reference:
                // here, we made use of the .chars(), .all() operator, which is extremely powerful
                // for checking whether multiple items satisfy a predicate
            if userinput_task_deadline_array[0].chars().all(char::is_numeric) && userinput_task_deadline_array[1].chars().all(char::is_numeric) && userinput_task_deadline_array[2].trim_end().chars().all(char::is_numeric) {
                // println!("awesome");
            } else {
                // println!("fk off");
                println!("Enter a valid integer input.\nEnter task deadline in the following format [DD/MM/YY]: ");
                continue;
            }

            // these have to be signed integers first, to allow for subsequent error checking
            let userinput_task_deadline_day_int:i32 = userinput_task_deadline_array[0].trim_end().parse().unwrap();
            let userinput_task_deadline_month_int:i32 = userinput_task_deadline_array[1].trim_end().parse().unwrap();
            let userinput_task_deadline_year_int:i32 = userinput_task_deadline_array[2].trim_end().parse().unwrap();
            
            // checking for valid date inputs
            if userinput_task_deadline_day_int > 31 || userinput_task_deadline_day_int < 1 {
                println!("Enter a valid day input.\nEnter task deadline in the following format [DD/MM/YY]: ");
                continue;
            }
            if userinput_task_deadline_month_int > 12 || userinput_task_deadline_month_int < 1 {
                println!("Enter a valid month input.\nEnter task deadline in the following format [DD/MM/YY]: ");
                continue;
            } 
            if userinput_task_deadline_year_int < 23 || userinput_task_deadline_year_int > 99 {
                println!("Enter a valid year input.\nEnter task deadline in the following format [DD/MM/YY]: ");
                continue; 
            }
            userinput_task_deadline_formatted = [userinput_task_deadline_day_int, userinput_task_deadline_month_int, userinput_task_deadline_year_int];
            break;
            // let [user_day, user_month, user_year] = userinput_task_deadline_formatted;
        }

        // task urgency handled by an enum
        println!("Enter task urgency (L/M/H): ");
        let userinput_task_urgency:UrgencyLevel;
        
        // loop and a match pattern to handle error handling
        loop {
            let mut userinput_task_urgency_string:String = String::new();
            io::stdin().read_line(&mut userinput_task_urgency_string).expect("Failed to read line");
            let userinput_task_urgency_stringliteral:&str = userinput_task_urgency_string.as_str().trim_end();
            match userinput_task_urgency_stringliteral {
                "l" => {
                    userinput_task_urgency = UrgencyLevel::Low;
                    break;
                },
                "m" => {
                    userinput_task_urgency = UrgencyLevel::Medium;
                    break;
                },
                "h" => {
                    userinput_task_urgency = UrgencyLevel::High;
                    break;
                },
                // match-all pattern employed for invalid input
                &_ => {
                    println!("Please enter a valid input! [L/M/H]: ");
                    }
                }
            }

        let given_task = Task {
            task_name: userinput_task_name,
            task_description: userinput_task_description,
            task_deadline: userinput_task_deadline_formatted,
            task_urgency: userinput_task_urgency,
        };

        //storage_vector.push((userinput_task_name, userinput_task_description, userinput_task_deadline_formatted, userinput_task_urgency));
        storage_vector.push(given_task);
        println!("{:?}", storage_vector);

        };
        
    }
