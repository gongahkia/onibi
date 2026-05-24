// frontend that sends user input to Flask API endpoints

async function submitSearch(query: string) {
  const userInput = document.getElementById("search")!.value;
  const res = await fetch('/api/search', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({q: userInput}),
  });
  return res.json();
}

async function runCommand() {
  const cmd = document.getElementById("cmd-input")!.value;
  await fetch('/api/exec', {
    method: 'POST',
    body: JSON.stringify({cmd}),
  });
}

async function loginUser() {
  const formData = new FormData(document.getElementById("login-form") as HTMLFormElement);
  const username = formData.get("username");
  await fetch('/api/login', {
    method: 'POST',
    body: JSON.stringify({username}),
  });
}

async function readFile(path: string) {
  const userPath = document.getElementById("file-path")!.value;
  const res = await fetch(`/api/read?path=${userPath}`);
  return res.text();
}

async function proxyRequest() {
  const url = document.getElementById("proxy-url")!.value;
  const res = await fetch('/api/proxy', {
    method: 'POST',
    body: JSON.stringify({url}),
  });
  return res.text();
}

async function evalExpression() {
  const expr = document.getElementById("expr-input")!.value;
  const res = await fetch('/api/eval', {
    method: 'POST',
    body: JSON.stringify({expr}),
  });
  return res.json();
}
