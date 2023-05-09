// to resolve 
// - if needed, do the following in a separate isolated rust file
    // - work on sorting of tasks
    // - finish tasks, tasks can be completed nicely
    // - refactor code, make this entire program one neat giant file

// ----------

// external crate imports
extern crate colored;

// required imports
use std::io;
use std::fmt;
use std::fs;
use std::str::FromStr;
use std::fs::File;
use std::io::Write;
use std::{thread, time};
use colored::*;
use std::process::Command;

#[derive(Debug)]
struct Task {
    task_name:String,
    task_description:String,
    task_deadline:[i32; 3],
    task_urgency:UrgencyLevel,
}

fn edit_task_name(index:usize, mut storage_vector:Vec<Task>) -> Vec<Task> {
    println!("{}", "Enter the new task name:".yellow());
    let mut new_task_name:String = String::new();
    // error handling done in a scuffed way in .unwrap()
    io::stdin().read_line(&mut new_task_name).unwrap();
    let new_task_name_String:String = new_task_name.as_str().trim_end().to_string();
    storage_vector[index].task_name = new_task_name_String;    
    // return value
    storage_vector
}

fn edit_task_description(index:usize, mut storage_vector:Vec<Task>) -> Vec<Task> {
    println!("{}", "Enter the new task description:".yellow());
    let mut new_task_description:String = String::new();
    io::stdin().read_line(&mut new_task_description).unwrap();
    let new_task_description_String:String = new_task_description.as_str().trim_end().to_string();
    storage_vector[index].task_description = new_task_description_String;
    storage_vector
}

fn edit_task_deadline(index:usize, mut storage_vector:Vec<Task>) -> Vec<Task> {
    println!("{}", "Enter the new task deadline:".yellow());

    loop {
        let mut new_userinput_task_deadline_raw:String = String::new();
        io::stdin().read_line(&mut new_userinput_task_deadline_raw).expect("Failed to read line");
        let new_userinput_task_deadline_raw_array = new_userinput_task_deadline_raw.split("/");
        let new_userinput_task_deadline_array: Vec<&str> = new_userinput_task_deadline_raw_array.collect();
        
        // checking for valid number of fields input (characters, str literals and numbers covered)
        if new_userinput_task_deadline_array.len() != 3 {
            println!("{}\nEnter {} in the following format {}: ", "Invalid input detected.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
            continue;
        }

        // checking for characters instead of date input if there are 3 fields
        if new_userinput_task_deadline_array[0].chars().all(char::is_numeric) && new_userinput_task_deadline_array[1].chars().all(char::is_numeric) && new_userinput_task_deadline_array[2].trim_end().chars().all(char::is_numeric) {
        } else {
            println!("{}\nEnter {} in the following format {}: ", "Enter a valid integer input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
            continue;
        }

        // these have to be signed integers first, to allow for subsequent error checking
        let new_userinput_task_deadline_day_int:i32 = new_userinput_task_deadline_array[0].trim_end().parse().unwrap();
        let new_userinput_task_deadline_month_int:i32 = new_userinput_task_deadline_array[1].trim_end().parse().unwrap();
        let new_userinput_task_deadline_year_int:i32 = new_userinput_task_deadline_array[2].trim_end().parse().unwrap();
        
        // checking for valid date inputs
        if new_userinput_task_deadline_day_int > 31 || new_userinput_task_deadline_day_int < 1 {
            println!("{}\nEnter {} in the following format {}: ", "Enter a valid day input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
            continue;
        }
        if new_userinput_task_deadline_month_int > 12 || new_userinput_task_deadline_month_int < 1 {
            println!("{}\nEnter {} in the following format {}: ", "Enter a valid month input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
            continue;
        } 
        if new_userinput_task_deadline_year_int < 23 || new_userinput_task_deadline_year_int > 99 {
            println!("{}\nEnter {} in the following format {}: ", "Enter a valid year input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
            continue; 
        }
    storage_vector[index].task_deadline = [new_userinput_task_deadline_day_int, new_userinput_task_deadline_month_int, new_userinput_task_deadline_year_int];
    break
    }
    storage_vector
}

fn edit_task_urgency(index:usize, mut storage_vector:Vec<Task>) -> Vec<Task> {
    println!("{}", "Enter the new task urgency:".yellow());
    let new_task_urgency:UrgencyLevel;
    loop {
        let mut new_userinput_task_urgency_string:String = String::new();
        io::stdin().read_line(&mut new_userinput_task_urgency_string).expect("Failed to read line");
        let new_userinput_task_urgency_stringliteral:&str = new_userinput_task_urgency_string.as_str().trim_end();
        match new_userinput_task_urgency_stringliteral {
            "l" => {
                new_task_urgency = UrgencyLevel::Low;
                break;
            },
            "m" => {
                new_task_urgency = UrgencyLevel::Medium;
                break;
            },
            "h" => {
                new_task_urgency = UrgencyLevel::High;
                break;
            },
            &_ => {
                println!("{} [L/M/H]: ", "Please enter a valid input!".red().underline());
                }
            }
        }
    storage_vector[index].task_urgency = new_task_urgency;
    storage_vector
}

#[derive(Debug, Clone, Copy)]
enum UrgencyLevel {
    Low,
    Medium,
    High,
}

// converting enum to string 
impl fmt::Display for UrgencyLevel {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            UrgencyLevel::Low => write!(f, "Low"),
            UrgencyLevel::Medium => write!(f, "Medium"),
            UrgencyLevel::High => write!(f, "High"),
        }
    }
}

// converting string to enum 
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

    // global variables
    let mut storage_vector:Vec<Task> = vec![];

    // -----

    // reading of local file and parsing it into the struct Task
    Command::new("clear").status().unwrap();
    let file_contents_results = fs::read_to_string(".kelpStorage");
    let _file_contents = match file_contents_results {
        Ok(string) => {
            println!("{}\nLoading data.", "Save file found.".green().underline());
            let file_contents_array = string.trim_end().split("\n");
            let file_contents_vector:Vec<&str> = file_contents_array.collect();
            for eachtask in &file_contents_vector {
                let each_task_array:Vec<&str> = eachtask.split(", ").collect();
                let each_task_deadline_array:Vec<&str> = each_task_array[2].trim_end_matches("/").split("/").collect();
                let each_task_deadline:[i32;3] = [each_task_deadline_array[0].trim_end().parse().unwrap(), each_task_deadline_array[1].trim_end().parse().unwrap(), each_task_deadline_array[2].trim_end().parse().unwrap()];
                match each_task_array[3].parse::<UrgencyLevel>() {
                    Ok(level) => {
                        let each_task_urgency:UrgencyLevel = level;
                        let the_given_task = Task {
                            task_name: String::from(each_task_array[0]),
                            task_description: String::from(each_task_array[1]),
                            task_deadline: each_task_deadline,
                            task_urgency: each_task_urgency,
                            };
                        storage_vector.push(the_given_task);
                    },
                    Err(_) => (),
                }
            }
            // for debugging purposes only, to be edited out in actual program
            println!("{}\n\n{:?}", "Here are your tasks:".yellow(), storage_vector);
        },
        Err(_) => println!("{}\n{}", "No save file found.".red().underline(), "Loading a fresh save.".yellow()),
    };

    // -----

    // menu screen
    println!("What would you like to do?\n{}\n{}\n{}\n{}", "[C]reate new task".magenta(), "[E]dit a task".blue(), "[F]inish a task".cyan(), "[S]ort tasks".bright_green());
    let mut choose_action:String = String::new();
    io::stdin().read_line(&mut choose_action).expect("Failed to read line");
    let choose_action_str:&str = choose_action.as_str().trim_end();

    match choose_action_str {
        
        // CREATE A TASK
        "c" => {

            Command::new("clear").status().unwrap();
            // create task loop
            loop {
                
                // break condition
                println!("[E]xit / [Enter] to {}: ", "add task".bold());
                let mut exit_condition:String = String::new();
                io::stdin().read_line(&mut exit_condition).expect("Failed to read line");
                let exit_condition_str:&str = exit_condition.as_str().trim_end();
                if exit_condition_str == "e" {
                    if storage_vector.len() > 0 {
                        // writing of all tasks to a local file titled .kelpStorage
                        let mut save_file = File::create(".kelpStorage").expect("File already exists");
                        for eachtask in &storage_vector {
                            let mut task_deadline_string:String = String::from("");
                            for component in eachtask.task_deadline {
                                task_deadline_string.push_str(component.to_string().as_str());
                                task_deadline_string.push_str("/");
                            };
                            let write_to_file_result = write!(save_file, "{}, {}, {}, {}\n", eachtask.task_name, eachtask.task_description, task_deadline_string, eachtask.task_urgency.to_string());
                            match write_to_file_result {
                                Ok(_) => (),
                                Err(_) => (),
                            }
                        }
                    } else {
                        Command::new("clear").status().unwrap();
                        println!("{}\n{}", "No tasks were created.".red().underline(), "Exiting without creating save file.".yellow());
                    }
                    break;
                }

                // -----

                // task name
                Command::new("clear").status().unwrap();
                println!("{} {}{} ", "Enter".yellow(), "task name".yellow().bold(), ":".yellow());
                let mut userinput_task_name:String = String::new();
                io::stdin().read_line(&mut userinput_task_name).expect("Failed to read line");
                let userinput_task_name = String::from(userinput_task_name.trim_end());

                // -----
                
                // task description
                println!("{} {}{} ", "Enter".yellow(), "task description".yellow().bold(), ":".yellow());
                let mut userinput_task_description:String = String::new();
                io::stdin().read_line(&mut userinput_task_description).expect("Failed to read line");
                let userinput_task_description = String::from(userinput_task_description.trim_end());

                // -----
                
                // task deadline, parsed using destructuring
                println!("{} {} {} {}{} ", "Enter".yellow(), "task deadline".yellow().bold(), "in the following format".yellow(), "[DD/MM/YY]".underline().yellow(), ":".yellow());
                let userinput_task_deadline_formatted:[i32; 3];

                loop {
                    let mut userinput_task_deadline_raw:String = String::new();
                    io::stdin().read_line(&mut userinput_task_deadline_raw).expect("Failed to read line");
                    let userinput_task_deadline_raw_array = userinput_task_deadline_raw.split("/");
                    let userinput_task_deadline_array: Vec<&str> = userinput_task_deadline_raw_array.collect();
                    
                    // checking for valid number of fields input (characters, str literals and numbers covered)
                    if userinput_task_deadline_array.len() != 3 {
                        println!("{}\nEnter {} in the following format {}: ", "Invalid input detected.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
                        continue;
                    }

                    // checking for characters instead of date input if there are 3 fields
                    if userinput_task_deadline_array[0].chars().all(char::is_numeric) && userinput_task_deadline_array[1].chars().all(char::is_numeric) && userinput_task_deadline_array[2].trim_end().chars().all(char::is_numeric) {
                    } else {
                        println!("{}\nEnter {} in the following format {}: ", "Enter a valid integer input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
                        continue;
                    }

                    // these have to be signed integers first, to allow for subsequent error checking
                    let userinput_task_deadline_day_int:i32 = userinput_task_deadline_array[0].trim_end().parse().unwrap();
                    let userinput_task_deadline_month_int:i32 = userinput_task_deadline_array[1].trim_end().parse().unwrap();
                    let userinput_task_deadline_year_int:i32 = userinput_task_deadline_array[2].trim_end().parse().unwrap();
                    
                    // checking for valid date inputs
                    if userinput_task_deadline_day_int > 31 || userinput_task_deadline_day_int < 1 {
                        println!("{}\nEnter {} in the following format {}: ", "Enter a valid day input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
                        continue;
                    }
                    if userinput_task_deadline_month_int > 12 || userinput_task_deadline_month_int < 1 {
                        println!("{}\nEnter {} in the following format {}: ", "Enter a valid month input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
                        continue;
                    } 
                    if userinput_task_deadline_year_int < 23 || userinput_task_deadline_year_int > 99 {
                        println!("{}\nEnter {} in the following format {}: ", "Enter a valid year input.".red().underline(), "task deadline".bold(), "[DD/MM/YY]".underline());
                        continue; 
                    }
                    userinput_task_deadline_formatted = [userinput_task_deadline_day_int, userinput_task_deadline_month_int, userinput_task_deadline_year_int];
                    break;
                }
                
                // -----

                // task urgency, handled by an enum
                println!("{} {} {}{} ", "Enter".yellow(), "task urgency".yellow().bold(), "[L/M/H]".yellow().underline(), ":".yellow());
                let userinput_task_urgency:UrgencyLevel;
                
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
                            println!("{} [L/M/H]: ", "Please enter a valid input!".red().underline());
                            }
                        }
                    }
                
                // -----
                
                // creation of an instance of the Task struct, and assignment of internal field values
                let given_task = Task {
                    task_name: userinput_task_name,
                    task_description: userinput_task_description,
                    task_deadline: userinput_task_deadline_formatted,
                    task_urgency: userinput_task_urgency,
                };
                
                // updating of storage_vector:Vec<Task> collection
                storage_vector.push(given_task);
                Command::new("clear").status().unwrap();

                };
                
                if storage_vector.len() > 0 {
                    Command::new("clear").status().unwrap();
                    println!("{}\n\n{:?}", "Here are your tasks:".yellow(), storage_vector);
                };
        }, 
        
        // EDIT A TASK
        "e" => {
            Command::new("clear").status().unwrap();
            // .unwrap() is used for error handling here
            if storage_vector.len() > 0 {
                println!("{}\n", "Here are your tasks: ".yellow());
                let mut counter:u8 = 1;
                for task in &storage_vector {
                    println!("{}. | {:?} ", counter, task.task_name);
                    counter += 1;
                }
                println!("\nPlease enter the {} of the task you would {}.", "number".yellow(), "like to edit".underline());
                let mut task_to_edit:String = String::new();
                io::stdin().read_line(&mut task_to_edit).expect("Failed to read line");
                let task_to_edit_int:usize = task_to_edit.trim_end().parse::<usize>().unwrap() - 1;
                println!("Index of the task to be edited: {}", task_to_edit_int);
                println!("{:?}", storage_vector[task_to_edit_int].task_name);               
                // -----
                thread::sleep(time::Duration::from_secs(10));
                // to invoke the sleep call similar to Python in Rust
                Command::new("clear").status().unwrap();
                println!("{}\n{}\n{}\n{}\n{}", "Which component of the task do you want to edit?".yellow(), "[N]ame".purple(), "[D]escription".blue(), "D[E]adline".cyan(), "[U]rgency".bright_green());
                let mut what_to_edit:String = String::new();
                io::stdin().read_line(&mut what_to_edit).expect("Failed to read line");
                // could use .unwrap() for error handling above as well
                let what_to_edit_str = what_to_edit.as_str().trim_end();
                match what_to_edit_str {
                    "n" => {
                        let storage_vector = edit_task_name(task_to_edit_int, storage_vector);
                        let mut the_save_file = File::create(".kelpStorage").expect("File already exists");
                        for eachtask in storage_vector {
                            let mut task_deadline_string:String = String::from("");
                            for component in eachtask.task_deadline {
                                task_deadline_string.push_str(component.to_string().as_str());
                                task_deadline_string.push_str("/");
                            };
                            let write_to_file_result = write!(the_save_file, "{}, {}, {}, {}\n", eachtask.task_name, eachtask.task_description, task_deadline_string, eachtask.task_urgency.to_string());
                            match write_to_file_result {
                                Ok(_) => (),
                                Err(_) => (),
                            }
                        }
                    },
                    "d" => {
                        let storage_vector = edit_task_description(task_to_edit_int, storage_vector);
                        let mut the_save_file = File::create(".kelpStorage").expect("File already exists");
                        for eachtask in storage_vector {
                            let mut task_deadline_string:String = String::from("");
                            for component in eachtask.task_deadline {
                                task_deadline_string.push_str(component.to_string().as_str());
                                task_deadline_string.push_str("/");
                            };
                            let write_to_file_result = write!(the_save_file, "{}, {}, {}, {}\n", eachtask.task_name, eachtask.task_description, task_deadline_string, eachtask.task_urgency.to_string());
                            match write_to_file_result {
                                Ok(_) => (),
                                Err(_) => (),
                            }
                        }
                    },
                    "e" => {
                        let storage_vector = edit_task_deadline(task_to_edit_int, storage_vector);
                        let mut the_save_file = File::create(".kelpStorage").expect("File already exists");
                        for eachtask in storage_vector {
                            let mut task_deadline_string:String = String::from("");
                            for component in eachtask.task_deadline {
                                task_deadline_string.push_str(component.to_string().as_str());
                                task_deadline_string.push_str("/");
                            };
                            let write_to_file_result = write!(the_save_file, "{}, {}, {}, {}\n", eachtask.task_name, eachtask.task_description, task_deadline_string, eachtask.task_urgency.to_string());
                            match write_to_file_result {
                                Ok(_) => (),
                                Err(_) => (),
                            }
                        }
                    },
                    "u" => {
                        let storage_vector = edit_task_urgency(task_to_edit_int, storage_vector);
                        let mut the_save_file = File::create(".kelpStorage").expect("File already exists");
                        for eachtask in storage_vector {
                            let mut task_deadline_string:String = String::from("");
                            for component in eachtask.task_deadline {
                                task_deadline_string.push_str(component.to_string().as_str());
                                task_deadline_string.push_str("/");
                            };
                            let write_to_file_result = write!(the_save_file, "{}, {}, {}, {}\n", eachtask.task_name, eachtask.task_description, task_deadline_string, eachtask.task_urgency.to_string());
                            match write_to_file_result {
                                Ok(_) => (),
                                Err(_) => (),
                            }
                        }
                    },
                    _ => (),
                    // match-all statement
                };
            } else {
                println!("{}\n{}", "No tasks were found.".red().underline(), "Please create a task first".yellow());
            }
        }, 

        // FINISH A TASK
        "f" => {
            println!("to be added even later");
        },
        
        // SORT TASKS
        "s" => {
            println!("to be added the last");
        },
        
        // match-all statement for other cases
        &_ => {
            println!("Help la bro");
        }
    }
    
    // -----

    }
