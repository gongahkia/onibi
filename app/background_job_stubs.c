#include <caml/alloc.h>
#include <caml/fail.h>
#include <caml/memory.h>
#include <caml/mlvalues.h>
#include <caml/unixsupport.h>

#include <errno.h>
#include <stdlib.h>
#include <unistd.h>

static char **string_vector(value vector) {
  mlsize_t count = Wosize_val(vector);
  char **values = calloc(count + 1, sizeof(*values));
  if (values == NULL) return NULL;
  for (mlsize_t index = 0; index < count; index++) {
    values[index] = String_val(Field(vector, index));
  }
  return values;
}

static void close_if_nonstandard(int descriptor) {
  if (descriptor > STDERR_FILENO) (void)close(descriptor);
}

CAMLprim value zenbu_background_job_spawn(value program, value argv,
                                          value environment, value stdin_fd,
                                          value stdout_fd, value stderr_fd,
                                          value cwd) {
  CAMLparam5(program, argv, environment, stdin_fd, stdout_fd);
  CAMLxparam2(stderr_fd, cwd);
  char **arguments = string_vector(argv);
  char **variables = string_vector(environment);
  if (arguments == NULL || variables == NULL) {
    free(arguments);
    free(variables);
    caml_failwith("background job: could not allocate process arguments");
  }

  pid_t pid = fork();
  if (pid == -1) {
    int saved_errno = errno;
    free(arguments);
    free(variables);
    errno = saved_errno;
    caml_uerror("fork", Nothing);
  }
  if (pid == 0) {
    int input = Int_val(stdin_fd);
    int output = Int_val(stdout_fd);
    int error = Int_val(stderr_fd);
    if (setsid() == -1 || chdir(String_val(cwd)) == -1 ||
        dup2(input, STDIN_FILENO) == -1 ||
        dup2(output, STDOUT_FILENO) == -1 ||
        dup2(error, STDERR_FILENO) == -1) {
      _exit(127);
    }
    close_if_nonstandard(input);
    close_if_nonstandard(output);
    close_if_nonstandard(error);
    execve(String_val(program), arguments, variables);
    _exit(127);
  }

  free(arguments);
  free(variables);
  CAMLreturn(Val_long(pid));
}

CAMLprim value zenbu_background_job_spawn_bytecode(value *arguments,
                                                   int argument_count) {
  if (argument_count != 7) {
    caml_invalid_argument("background job spawn arity");
  }
  return zenbu_background_job_spawn(arguments[0], arguments[1], arguments[2],
                                    arguments[3], arguments[4], arguments[5],
                                    arguments[6]);
}
