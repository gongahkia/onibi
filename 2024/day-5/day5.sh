#!/bin/bash

# (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻ 
# (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻ 
# THIS DOES NOT WORK, I AM A DUMB DUMB WHO CANNOT DEBUG THIS AAAAAAAAAA :(((
# (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻ 
# (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻  (╯°□°）╯︵ ┻━┻ 

parse() {
    target_filepath="$1"
    rules=()
    updates=()
    while IFS= read -r line; do
        if [[ -z "$line" ]]; then
            break
        fi
        rules+=("$line")
    done < "$target_filepath"
    while IFS= read -r line; do
        updates+=("$line")
    done < "$target_filepath"
    rule_array=()
    for rule in "${rules[@]}"; do
        x=$(echo "$rule" | cut -d'|' -f1)
        y=$(echo "$rule" | cut -d'|' -f2)
        rule_array+=("$x,$y")
    done
    update_array=()
    for update in "${updates[@]}"; do
        update_array+=("$(echo "$update" | tr ',' ' ')")
    done
}

valid() {
    update=($1)
    valid=true
    for rule in "${rule_array[@]}"; do
        x=$(echo "$rule" | cut -d',' -f1)
        y=$(echo "$rule" | cut -d',' -f2)
        if [[ " ${update[@]} " =~ " $x " && " ${update[@]} " =~ " $y " ]]; then
            x_index=$(echo "${update[@]}" | tr ' ' '\n' | grep -n -m 1 "^$x$" | cut -d: -f1)
            y_index=$(echo "${update[@]}" | tr ' ' '\n' | grep -n -m 1 "^$y$" | cut -d: -f1)
            if [[ $x_index -gt $y_index ]]; then
                valid=false
                break
            fi
        fi
    done
    echo $valid
}

topsort() {
    update=($1)
    declare -A graph
    declare -A in_deg
    for rule in "${rule_array[@]}"; do
        x=$(echo "$rule" | cut -d',' -f1)
        y=$(echo "$rule" | cut -d',' -f2)
        if [[ " ${update[@]} " =~ " $x " && " ${update[@]} " =~ " $y " ]]; then
            graph["$x"]="${graph[$x]} $y"
            ((in_deg["$y"]++))
            if [[ -z "${in_deg[$x]}" ]]; then
                in_deg["$x"]=0
            fi
        fi
    done
    queue=()
    for node in "${update[@]}"; do
        if [[ ${in_deg[$node]} -eq 0 ]]; then
            queue+=("$node")
        fi
    done
    sorted_order=()
    while [[ ${#queue[@]} -gt 0 ]]; do
        node=${queue[0]}
        queue=("${queue[@]:1}")
        sorted_order+=("$node")
        for neighbor in ${graph["$node"]}; do
            ((in_deg["$neighbor"]--))
            if [[ ${in_deg[$neighbor]} -eq 0 ]]; then
                queue+=("$neighbor")
            fi
        done
    done
    if [[ ${#sorted_order[@]} -ne ${#update[@]} ]]; then
        echo ""
    else
        echo "${sorted_order[@]}"
    fi
}

mid_sum() {
    target_filepath="$1"
    parse "$target_filepath"
    part_1_result=0
    for update in "${update_array[@]}"; do
        valid_result=$(valid "$update")
        if [[ $valid_result == "true" ]]; then
            update_array_int=($update)
            mid_index=$((${#update_array_int[@]} / 2))
            part_1_result=$((part_1_result + ${update_array_int[$mid_index]}))
        fi
    done
    part_2_result=0
    for update in "${update_array[@]}"; do
        valid_result=$(valid "$update")
        if [[ $valid_result == "false" ]]; then
            ordered_update=$(topsort "$update")
            if [[ -n "$ordered_update" ]]; then
                ordered_update_array=($ordered_update)
                mid_index=$((${#ordered_update_array[@]} / 2))
                part_2_result=$((part_2_result + ${ordered_update_array[$mid_index]}))
            fi
        fi
    done
    echo "part a: $part_1_result"
    echo "part b: $part_2_result"
}

mid_sum "input-1.txt"