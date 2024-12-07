abstract EquationGrammar = {

  flags startcat = Input;

  cat
    Input;    
    Equation;
    TestValue;
    Numbers; 
    Expression
    Term;     
    Operator;
    Integer;
    Digit;
    Newline;  

  fun

    Input : List Equation -> Input;
    Equation : TestValue -> Numbers -> Newline -> Equation;

    TestValue : Integer -> TestValue;
    Numbers : List Integer -> Numbers;
    Newline : Newline;

    Expression : Term -> List Operator -> Expression;
    Term : Integer -> Term;

    Plus : Operator;
    Multiply : Operator;
    Concat : Operator;

    Integer : List Digit -> Integer;
    Digit0 : Digit;
    Digit1 : Digit;
    Digit2 : Digit;
    Digit3 : Digit;
    Digit4 : Digit;
    Digit5 : Digit;
    Digit6 : Digit;
    Digit7 : Digit;
    Digit8 : Digit;
    Digit9 : Digit;
    
}