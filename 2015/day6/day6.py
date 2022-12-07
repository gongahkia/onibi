def part1 ():
	permdict = {}
	for x in range (1000):
		for y in range (1000):
			permdict [(x,y)] = 0
		#light state off = 0, on=1
		
	fhand = open ('input.txt','r')
	for line in fhand:
		line = line.rstrip()
		components = line.split(' ')
		if len(components) == 4:
			#TOGGLE
			action = 'toggle'
			startcoord = components [1]
			endcoord = components [3]
			
		else:
			#ON/OFF
			action = components [1]
			startcoord = components [2]
			endcoord = components [4]
			
		startx = int(startcoord.split(',')[0])
		endx = int(endcoord.split(',')[0])
		starty = int(startcoord.split(',')[1])
		endy = int(endcoord.split(',')[1])
		
		for x in range (startx,endx+1):
			for y in range (starty,endy+1):
				#print ((x,y))
			
				if action == 'on':
					permdict [(x,y)] = 1
				
				elif action == 'off':
					permdict [(x,y)] = 0
					
				elif action == 'toggle':
					if permdict [(x,y)] == 0:
						permdict [(x,y)] = 1
					else: 
						permdict [(x,y)] = 0
	
	counter = 0
	for light,state in permdict.items():
		if state == 1:
			counter += 1

	return counter 

def part2 ():
	permdict = {}
	for x in range (1000):
		for y in range (1000):
			permdict [(x,y)] = 0
		#light state off = 0, on=1
		
	fhand = open ('input.txt','r')
	for line in fhand:
		line = line.rstrip()
		components = line.split(' ')
		if len(components) == 4:
			#TOGGLE
			action = 'toggle'
			startcoord = components [1]
			endcoord = components [3]
			
		else:
			#ON/OFF
			action = components [1]
			startcoord = components [2]
			endcoord = components [4]
			
		startx = int(startcoord.split(',')[0])
		endx = int(endcoord.split(',')[0])
		starty = int(startcoord.split(',')[1])
		endy = int(endcoord.split(',')[1])
		
		for x in range (startx,endx+1):
			for y in range (starty,endy+1):
				#print ((x,y))
			
				if action == 'on':
					permdict [(x,y)] = permdict [(x,y)] + 1
				
				elif action == 'off':
					if permdict [(x,y)] == 0:
						continue
					permdict [(x,y)] = permdict [(x,y)] - 1
					
				elif action == 'toggle':
					permdict [(x,y)] = permdict [(x,y)] + 2
	
	brightness = 0
	for light,state in permdict.items():
		brightness += state

	return brightness 

print(part1())
print(part2())
		