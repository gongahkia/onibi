// to resolve:
// - add a break condition that ends the loop
// - if needed, do the following in a separate isolated rust file
// - debug the match statement at bottom of program that is commented out
// - add the enum choosing back in, figure out why the enum cant be chosen
// - destructure the data presented into the Task struct if possible
// - add error checking for invalid input for the date, the urgency level

use std::io;

// destructure the tuple later to edit its contents
struct Task {
    task_name:String, // eg. do math homework
    task_description:String, // eg. submit them via managebac after completing them via linkedin
    task_deadline:[i32; 3],
    task_urgency:UrgencyLevel,
}

enum UrgencyLevel {
    Low,
    Medium,
    High,
}

fn main() {

    let mut storage_array:Vec<(String, String, [u32;3], &str)> = vec![];

    loop {
        
        // * to add a break condition here!

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
        println!("Enter task deadline in the following format [DD/MM/YY]: ");
        let mut userinput_task_deadline_raw:String = String::new();
        io::stdin().read_line(&mut userinput_task_deadline_raw).expect("Failed to read line");
        let userinput_task_deadline_raw_array = userinput_task_deadline_raw.split("/");
        let userinput_task_deadline_array: Vec<&str> = userinput_task_deadline_raw_array.collect();
        
        // for future reference:
        // - error was initially occuring due to newline character of last element in vector, need
        // to remember to use .trim_end() method to remove said newline character
        let userinput_task_deadline_day_int:u32 = userinput_task_deadline_array[0].trim_end().parse().unwrap();
        let userinput_task_deadline_month_int:u32 = userinput_task_deadline_array[1].trim_end().parse().unwrap();
        let userinput_task_deadline_year_int:u32 = userinput_task_deadline_array[2].trim_end().parse().unwrap();

        let userinput_task_deadline_formatted:[u32; 3] = [userinput_task_deadline_day_int, userinput_task_deadline_month_int, userinput_task_deadline_year_int];

        // task urgency -> parsed into an enum
        println!("Enter task urgency (L/M/H): ");
        let mut userinput_task_urgency_string:String = String::new();
        io::stdin().read_line(&mut userinput_task_urgency_string).expect("Failed to read line");
        let userinput_task_urgency_stringliteral:&str = &userinput_task_urgency_string[..];

        // * edit this type to be that of an enum again after working out why the match statement
        // below does not work
        let userinput_task_urgency:&str = "shit";
    
        // * figure out why this match statement isnt working
        /*match userinput_task_urgency_stringliteral {
            "l" => {
                userinput_task_urgency = UrgencyLevel::Low;
            }, 
            "m" => {
                userinput_task_urgency = UrgencyLevel::Medium;
            },
            "h" => {
                userinput_task_urgency = UrgencyLevel::High;
            },
            _ => {
                println!("Defaulting to low task urgency!");
                userinput_task_urgency = UrgencyLevel::Low;
            },
        }*/
        
        storage_array.push((userinput_task_name, userinput_task_description, userinput_task_deadline_formatted, userinput_task_urgency));
        println!("{:?}", storage_array);
    }
    
}
