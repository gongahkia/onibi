"this does not run for part b because i am dumb as rocks"

(defun parse (filename)
  (with-open-file (stream filename :direction :input)
    (loop for line = (read-line stream nil)
          while line
          for (left right) = (mapcar #'parse-integer (split-sequence:split-sequence #\Space line :remove-empty-subseqs t))
          collect left into left-list
          collect right into right-list
          finally (return (values left-list right-list)))))

(defun total-distance (left-list right-list)
  (let* ((sorted-left (sort (copy-seq left-list) #'<))
         (sorted-right (sort (copy-seq right-list) #'<)))
    (reduce #'+ (mapcar (lambda (l r) (abs (- l r))) sorted-left sorted-right))))

(defun similarity-score (left-list right-list)
  (let ((count-map (make-hash-table)))
    (dolist (num right-list) (incf (gethash num count-map 0)))
    (reduce #'+ (mapcar (lambda (num) (* num (gethash num count-map 0))) left-list))))

(defun main ()
  (multiple-value-bind (left-list right-list) (parse "input-1.txt")
    (let ((part-a (total-distance left-list right-list))
          (part-b (similarity-score left-list right-list)))
      (format t "Part A: ~a~%Part B: ~a~%" part-a part-b))))